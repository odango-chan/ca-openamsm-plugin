package manifest_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/odango-chan/ca-openamsm-plugin/internal/event"
	"github.com/odango-chan/ca-openamsm-plugin/internal/manifest"
	"github.com/odango-chan/ca-openamsm-plugin/internal/plugin"
	"github.com/odango-chan/ca-openamsm-plugin/internal/slot"
	"github.com/odango-chan/ca-openamsm-plugin/internal/testsupport"
)

func loadRepositoryPackage(t *testing.T) *manifest.Package {
	t.Helper()
	pkg, err := manifest.Load("../..")
	if err != nil {
		t.Fatalf("load repository manifest: %v", err)
	}
	return pkg
}

func TestOAP01ManifestConformance(t *testing.T) {
	pkg := loadRepositoryPackage(t)
	if pkg.APIVersion != manifest.PackageAPIVersion || pkg.Kind != manifest.KindPlugin {
		t.Fatalf("package identity = %q/%q", pkg.APIVersion, pkg.Kind)
	}
	if pkg.PluginID != plugin.DefaultManifest.PluginID {
		t.Fatalf("plugin id = %q, binary id = %q", pkg.PluginID, plugin.DefaultManifest.PluginID)
	}
	if pkg.Version != plugin.DefaultManifest.Version {
		t.Fatalf("version = %q, binary version = %q", pkg.Version, plugin.DefaultManifest.Version)
	}
	if pkg.Protocol != manifest.IPCVersion {
		t.Fatalf("protocol = %q", pkg.Protocol)
	}
	if pkg.ToolsInterfacePackage != "" {
		t.Fatalf("event/delivery plugin must not declare tools")
	}

	declared := append([]string(nil), pkg.Extensions...)
	expected := append([]string(nil), plugin.DefaultManifest.Extensions...)
	sort.Strings(declared)
	sort.Strings(expected)
	if !reflect.DeepEqual(declared, expected) {
		t.Fatalf("extensions = %v, binary = %v", declared, expected)
	}

	eventTypes := map[string]bool{}
	for _, binding := range pkg.Events {
		eventTypes[binding.Type] = true
	}
	for _, want := range []string{event.TypeCreate, event.TypeUpdate, event.TypeContinuation} {
		if !eventTypes[want] {
			t.Fatalf("manifest misses event type %q", want)
		}
	}
	if len(pkg.Events) != 3 {
		t.Fatalf("events = %d", len(pkg.Events))
	}
	if len(pkg.Responses) != 3 {
		t.Fatalf("responses = %d", len(pkg.Responses))
	}

	expectedSlots := map[string]struct {
		slots    []string
		required []bool
	}{
		event.TypeCreate:       {slots: []string{slot.N1Request, slot.N2Request, slot.N2Ext1Request}, required: []bool{true, false, false}},
		event.TypeUpdate:       {slots: []string{slot.N1Request, slot.N2Request, slot.N2Ext1Request}, required: []bool{true, false, false}},
		event.TypeContinuation: {slots: []string{slot.N1Confirmation, slot.N2Confirmation}, required: []bool{false, false}},
	}
	for _, binding := range pkg.Events {
		want, ok := expectedSlots[binding.Type]
		if !ok {
			t.Fatalf("unexpected event %q", binding.Type)
		}
		if len(binding.Input) != len(want.slots) {
			t.Fatalf("%s input slots = %d", binding.Type, len(binding.Input))
		}
		for index, name := range want.slots {
			if binding.Input[index].Slot != name {
				t.Fatalf("%s slot[%d] = %q, want %q", binding.Type, index, binding.Input[index].Slot, name)
			}
			expectedMedia, _ := slot.MediaType(name)
			if binding.Input[index].MediaType != expectedMedia {
				t.Fatalf("%s slot[%d] media type = %q", binding.Type, index, binding.Input[index].MediaType)
			}
			if binding.Input[index].Required != want.required[index] {
				t.Fatalf("%s slot[%d] required = %v", binding.Type, index, binding.Input[index].Required)
			}
		}
	}

	for _, response := range pkg.Responses {
		switch response.EventType {
		case event.TypeCreate, event.TypeUpdate:
			if len(response.Output) != 2 {
				t.Fatalf("%s output slots = %d", response.EventType, len(response.Output))
			}
			if response.Output[0].Slot != slot.N1Response || response.Output[1].Slot != slot.N2Response {
				t.Fatalf("%s output slots = %+v", response.EventType, response.Output)
			}
		case event.TypeContinuation:
			if len(response.Output) != 0 {
				t.Fatalf("continuation must declare no output slots")
			}
		}
	}
}

func TestOAP01SchemaContractMatchesEmittedPayloads(t *testing.T) {
	pkg := loadRepositoryPackage(t)
	schemas := map[string]map[string]any{}
	for _, binding := range pkg.Events {
		schemas[binding.Type] = binding.Schema
	}

	fixtures := map[string]event.Event{}
	create, err := event.Normalize(event.Input{
		Operation: event.OperationCreate, Request: testsupport.CreateJSON(),
		N1: &event.BinaryPart{ContentID: "n1SmMsg", MediaType: slot.MediaTypeNAS, Bytes: testsupport.N1Modify},
	}, nil)
	if err != nil {
		t.Fatalf("create normalize: %v", err)
	}
	fixtures[event.TypeCreate] = create
	update, err := event.Normalize(event.Input{
		Operation: event.OperationUpdate, SmContextRef: "ref-1", Request: testsupport.UpdateJSONModify(),
		N1: &event.BinaryPart{ContentID: "n1SmMsg", MediaType: slot.MediaTypeNAS, Bytes: testsupport.N1Modify},
	}, event.TestDoubleClassifier{})
	if err != nil {
		t.Fatalf("update normalize: %v", err)
	}
	fixtures[event.TypeUpdate] = update
	continuation, err := event.Normalize(event.Input{
		Operation: event.OperationUpdate, SmContextRef: "ref-1", Request: testsupport.UpdateContinuationJSON(),
		N1: &event.BinaryPart{ContentID: "n1SmMsg", MediaType: slot.MediaTypeNAS, Bytes: testsupport.N1ReleaseComplete},
	}, event.TestDoubleClassifier{})
	if err != nil {
		t.Fatalf("continuation normalize: %v", err)
	}
	fixtures[event.TypeContinuation] = continuation

	for eventType, normalized := range fixtures {
		payload := toJSONValue(t, normalized.Payload)
		if err := checkSchema(schemas[eventType], payload, "$"); err != nil {
			t.Fatalf("%s payload does not match requestSchema: %v", eventType, err)
		}
	}
}

func TestOAP01SchemaOperationIDsAndDiscriminators(t *testing.T) {
	pkg := loadRepositoryPackage(t)
	for _, binding := range pkg.Events {
		properties, _ := binding.Schema["properties"].(map[string]any)
		operationSchema, _ := properties["operationId"].(map[string]any)
		if operationSchema == nil {
			t.Fatalf("%s has no operationId schema", binding.Type)
		}
		expected := event.OperationCreate
		if binding.Type != event.TypeCreate {
			expected = event.OperationUpdate
		}
		if operationSchema["const"] != expected {
			t.Fatalf("%s operationId const = %v", binding.Type, operationSchema["const"])
		}
	}
	if got := len(pkg.Events[0].Input); got != 3 {
		t.Fatalf("create input slots = %d", got)
	}
}

func TestOAP01RejectUnknownManifestField(t *testing.T) {
	root := writePackage(t, baseManifest()+"\nunexpectedField: true\n", nil)
	if _, err := manifest.Load(root); err == nil {
		t.Fatalf("unknown manifest field must fail")
	}
}

func TestOAP01RejectPathEscape(t *testing.T) {
	manifestText := baseManifest()
	manifestText = replaceOnce(manifestText, "schemas/echo-request.json", "../../escape.json")
	root := writePackage(t, manifestText, nil)
	if _, err := manifest.Load(root); err == nil {
		t.Fatalf("escaping schema path must fail")
	}
}

func TestOAP01RejectUndeclaredResponseEventType(t *testing.T) {
	manifestText := baseManifest()
	manifestText = replaceOnce(manifestText, "eventType: nsmf.create-sm-context", "eventType: nsmf.undeclared")
	root := writePackage(t, manifestText, nil)
	if _, err := manifest.Load(root); err == nil {
		t.Fatalf("undeclared response event type must fail")
	}
}

func TestOAP01RejectSlotMediaTypeMismatch(t *testing.T) {
	manifestText := baseManifest()
	manifestText = replaceOnce(manifestText, "mediaType: application/vnd.3gpp.5gnas", "mediaType: application/vnd.3gpp.ngap")
	root := writePackage(t, manifestText, nil)
	if _, err := manifest.Load(root); err == nil {
		t.Fatalf("slot/media mismatch must fail")
	}
}

func TestOAP01RejectDuplicateEventType(t *testing.T) {
	manifestText := baseManifest()
	manifestText = replaceOnce(manifestText, "- type: nsmf.create-sm-context", "- type: nsmf.create-sm-context\n      - type: nsmf.create-sm-context")
	root := writePackage(t, manifestText, nil)
	if _, err := manifest.Load(root); err == nil {
		t.Fatalf("duplicate event type must fail")
	}
}

const minimalSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["text"],"properties":{"text":{"type":"string"}}}`

func baseManifest() string {
	return `apiVersion: connectionagent.io/plugin-package/v1alpha1
kind: Plugin
metadata:
  id: example.test
  version: 0.0.1
process:
  executable: bin/plugin
  args: [serve]
  protocol: connectionagent.io/plugin-ipc/v1alpha1
extensions:
  event.ingress:
    events:
      - type: nsmf.create-sm-context
        requestSchema: schemas/echo-request.json
        artifacts:
          input:
            - slot: n1.request
              mediaType: application/vnd.3gpp.5gnas
              required: true
  event.delivery:
    responses:
      - eventType: nsmf.create-sm-context
        responseSchema: schemas/echo-response.json
`
}

func writePackage(t *testing.T, manifestText string, extraFiles map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "schemas"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "plugin.yaml"), []byte(manifestText), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "schemas", "echo-request.json"), []byte(minimalSchema), 0o600); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "schemas", "echo-response.json"), []byte(minimalSchema), 0o600); err != nil {
		t.Fatalf("write response schema: %v", err)
	}
	for name, content := range extraFiles {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write extra: %v", err)
		}
	}
	return root
}

func replaceOnce(text, old, replacement string) string {
	index := len(text)
	for position := 0; position+len(old) <= len(text); position++ {
		if text[position:position+len(old)] == old {
			index = position
			break
		}
	}
	if index == len(text) {
		panic(fmt.Sprintf("pattern %q not found", old))
	}
	return text[:index] + replacement + text[index+len(old):]
}

func toJSONValue(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return value
}

// checkSchema implements the small JSON Schema subset used by the repository
// schemas. Full Draft 2020-12 validation is Host-owned; this test asserts that
// the emitted payload satisfies the declared closed object.
func checkSchema(schema map[string]any, value any, where string) error {
	if schema == nil {
		return fmt.Errorf("%s: no schema", where)
	}
	if rawType, ok := schema["type"]; ok {
		if !matchesType(rawType, value) {
			return fmt.Errorf("%s: type mismatch for %v", where, value)
		}
	}
	if constant, ok := schema["const"]; ok && !reflect.DeepEqual(constant, value) {
		return fmt.Errorf("%s: const %v != %v", where, constant, value)
	}
	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, candidate := range enum {
			if reflect.DeepEqual(candidate, value) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: %v not in enum", where, value)
		}
	}
	if anyOf, ok := schema["anyOf"].([]any); ok {
		matched := false
		for _, candidate := range anyOf {
			if sub, ok := candidate.(map[string]any); ok && checkSchema(sub, value, where+"(anyOf)") == nil {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s: no anyOf branch matched", where)
		}
	}
	if object, ok := value.(map[string]any); ok {
		if minimum, ok := schema["minProperties"].(float64); ok && float64(len(object)) < minimum {
			return fmt.Errorf("%s: too few properties", where)
		}
		if required, ok := schema["required"].([]any); ok {
			for _, name := range required {
				if _, present := object[fmt.Sprint(name)]; !present {
					return fmt.Errorf("%s: missing required %v", where, name)
				}
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
			for key := range object {
				if _, declared := properties[key]; !declared {
					return fmt.Errorf("%s: undeclared property %q", where, key)
				}
			}
		}
		for key, sub := range properties {
			if child, present := object[key]; present {
				if subSchema, ok := sub.(map[string]any); ok {
					if err := checkSchema(subSchema, child, where+"."+key); err != nil {
						return err
					}
				}
			}
		}
	}
	if text, ok := value.(string); ok {
		if minimum, ok := schema["minLength"].(float64); ok && float64(len(text)) < minimum {
			return fmt.Errorf("%s: string too short", where)
		}
	}
	return nil
}

func matchesType(rawType any, value any) bool {
	names := []string{}
	switch typed := rawType.(type) {
	case string:
		names = []string{typed}
	case []any:
		for _, name := range typed {
			names = append(names, fmt.Sprint(name))
		}
	}
	for _, name := range names {
		switch name {
		case "object":
			if _, ok := value.(map[string]any); ok {
				return true
			}
		case "array":
			if _, ok := value.([]any); ok {
				return true
			}
		case "string":
			if _, ok := value.(string); ok {
				return true
			}
		case "number":
			if _, ok := value.(float64); ok {
				return true
			}
		case "boolean":
			if _, ok := value.(bool); ok {
				return true
			}
		case "null":
			if value == nil {
				return true
			}
		}
	}
	return false
}

var _ = errors.As
