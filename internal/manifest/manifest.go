// Package manifest validates the Plugin Package v1alpha1 manifest of this
// repository. It is used by conformance tests (OAP-01) and documents the exact
// declarative contract the binary implements.
package manifest

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/odango-chan/ca-openamsm-plugin/internal/slot"
)

const (
	// PackageAPIVersion is the only accepted package API version.
	PackageAPIVersion = "connectionagent.io/plugin-package/v1alpha1"
	// IPCVersion is the only accepted process protocol version.
	IPCVersion = "connectionagent.io/plugin-ipc/v1alpha1"
	// KindPlugin is the only accepted manifest kind.
	KindPlugin = "Plugin"

	ExtensionIngress  = "event.ingress"
	ExtensionDelivery = "event.delivery"
	ExtensionTools    = "tools"
)

// Error is a manifest validation failure with a stable code.
type Error struct {
	Code   string
	Detail string
	Err    error
}

func (e *Error) Error() string {
	message := e.Code
	if e.Detail != "" {
		message += ": " + e.Detail
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

func (e *Error) Unwrap() error { return e.Err }

// Validation codes.
const (
	CodeManifestUnreadable = "MANIFEST_UNREADABLE"
	CodeManifestInvalid    = "MANIFEST_INVALID"
	CodeAPIVersion         = "MANIFEST_API_VERSION"
	CodeKind               = "MANIFEST_KIND"
	CodeIdentity           = "MANIFEST_IDENTITY"
	CodeProtocol           = "MANIFEST_PROTOCOL"
	CodePathEscape         = "MANIFEST_PATH_ESCAPE"
	CodePathMissing        = "MANIFEST_PATH_MISSING"
	CodeExtensionUnknown   = "MANIFEST_EXTENSION_UNKNOWN"
	CodeEventInvalid       = "MANIFEST_EVENT_INVALID"
	CodeSlotInvalid        = "MANIFEST_SLOT_INVALID"
	CodeMediaTypeInvalid   = "MANIFEST_MEDIA_TYPE_INVALID"
	CodeSchemaInvalid      = "MANIFEST_SCHEMA_INVALID"
)

// Artifact is one declared artifact slot.
type Artifact struct {
	Slot      string
	MediaType string
	Required  bool
}

// Event is one declared ingress Event.
type Event struct {
	Type          string
	RequestSchema string
	Input         []Artifact
	Schema        map[string]any
}

// Response is one declared delivery response.
type Response struct {
	EventType      string
	ResponseSchema string
	Output         []Artifact
	Schema         map[string]any
}

// Package is a validated manifest plus its parsed schemas.
type Package struct {
	Root                  string
	APIVersion            string
	Kind                  string
	PluginID              string
	Version               string
	Executable            string
	Args                  []string
	Protocol              string
	Extensions            []string
	Events                []Event
	Responses             []Response
	ToolsInterfacePackage string
}

type artifactDoc struct {
	Slot      string `yaml:"slot"`
	MediaType string `yaml:"mediaType"`
	Required  *bool  `yaml:"required"`
}

type eventDoc struct {
	Type          string `yaml:"type"`
	Description   string `yaml:"description"`
	RequestSchema string `yaml:"requestSchema"`
	Artifacts     *struct {
		Input []artifactDoc `yaml:"input"`
	} `yaml:"artifacts"`
}

type responseDoc struct {
	EventType      string `yaml:"eventType"`
	ResponseSchema string `yaml:"responseSchema"`
	Artifacts      *struct {
		Output []artifactDoc `yaml:"output"`
	} `yaml:"artifacts"`
}

type document struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		ID      string `yaml:"id"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
	Process struct {
		Executable string   `yaml:"executable"`
		Args       []string `yaml:"args"`
		Protocol   string   `yaml:"protocol"`
	} `yaml:"process"`
	Extensions struct {
		EventIngress *struct {
			Events []eventDoc `yaml:"events"`
		} `yaml:"event.ingress"`
		EventDelivery *struct {
			Responses []responseDoc `yaml:"responses"`
		} `yaml:"event.delivery"`
		Tools *struct {
			InterfacePackage string `yaml:"interfacePackage"`
		} `yaml:"tools"`
	} `yaml:"extensions"`
}

var (
	pluginIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
	versionPattern  = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]*$`)
)

// Load validates the manifest at root/plugin.yaml.
func Load(root string) (*Package, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, &Error{Code: CodeManifestUnreadable, Err: err}
	}
	path := filepath.Join(absoluteRoot, "plugin.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Code: CodeManifestUnreadable, Detail: "plugin.yaml", Err: err}
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	var doc document
	if err := decoder.Decode(&doc); err != nil {
		return nil, &Error{Code: CodeManifestInvalid, Err: err}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, &Error{Code: CodeManifestInvalid, Detail: "multiple YAML documents"}
	}

	pkg := &Package{
		Root:       absoluteRoot,
		APIVersion: doc.APIVersion,
		Kind:       doc.Kind,
		PluginID:   doc.Metadata.ID,
		Version:    doc.Metadata.Version,
		Executable: doc.Process.Executable,
		Args:       doc.Process.Args,
		Protocol:   doc.Process.Protocol,
	}
	if doc.APIVersion != PackageAPIVersion {
		return nil, &Error{Code: CodeAPIVersion, Detail: doc.APIVersion}
	}
	if doc.Kind != KindPlugin {
		return nil, &Error{Code: CodeKind, Detail: doc.Kind}
	}
	if !pluginIDPattern.MatchString(pkg.PluginID) {
		return nil, &Error{Code: CodeIdentity, Detail: "metadata.id"}
	}
	if !versionPattern.MatchString(pkg.Version) {
		return nil, &Error{Code: CodeIdentity, Detail: "metadata.version"}
	}
	if pkg.Protocol != IPCVersion {
		return nil, &Error{Code: CodeProtocol, Detail: pkg.Protocol}
	}
	if _, err := relativeFile(absoluteRoot, pkg.Executable, false); err != nil {
		return nil, err
	}

	if doc.Extensions.EventIngress != nil {
		pkg.Extensions = append(pkg.Extensions, ExtensionIngress)
	}
	if doc.Extensions.EventDelivery != nil {
		pkg.Extensions = append(pkg.Extensions, ExtensionDelivery)
	}
	if doc.Extensions.Tools != nil {
		pkg.Extensions = append(pkg.Extensions, ExtensionTools)
	}
	if len(pkg.Extensions) == 0 {
		return nil, &Error{Code: CodeManifestInvalid, Detail: "extensions"}
	}

	if doc.Extensions.EventIngress != nil {
		seen := map[string]struct{}{}
		for _, event := range doc.Extensions.EventIngress.Events {
			if strings.TrimSpace(event.Type) == "" {
				return nil, &Error{Code: CodeEventInvalid, Detail: "type"}
			}
			if _, duplicate := seen[event.Type]; duplicate {
				return nil, &Error{Code: CodeEventInvalid, Detail: "duplicate " + event.Type}
			}
			seen[event.Type] = struct{}{}
			schemaPath, err := relativeFile(absoluteRoot, event.RequestSchema, true)
			if err != nil {
				return nil, err
			}
			schema, err := loadSchema(schemaPath)
			if err != nil {
				return nil, err
			}
			declared := Event{Type: event.Type, RequestSchema: event.RequestSchema, Schema: schema}
			slotSeen := map[string]struct{}{}
			if event.Artifacts != nil {
				for _, artifact := range event.Artifacts.Input {
					if !slot.IsInbound(artifact.Slot) {
						return nil, &Error{Code: CodeSlotInvalid, Detail: artifact.Slot}
					}
					expected, _ := slot.MediaType(artifact.Slot)
					if artifact.MediaType != expected {
						return nil, &Error{Code: CodeMediaTypeInvalid, Detail: artifact.Slot}
					}
					if artifact.Required == nil {
						return nil, &Error{Code: CodeEventInvalid, Detail: "required missing for " + artifact.Slot}
					}
					if _, duplicate := slotSeen[artifact.Slot]; duplicate {
						return nil, &Error{Code: CodeSlotInvalid, Detail: "duplicate " + artifact.Slot}
					}
					slotSeen[artifact.Slot] = struct{}{}
					declared.Input = append(declared.Input, Artifact{Slot: artifact.Slot, MediaType: artifact.MediaType, Required: *artifact.Required})
				}
			}
			pkg.Events = append(pkg.Events, declared)
		}
		if len(pkg.Events) == 0 {
			return nil, &Error{Code: CodeEventInvalid, Detail: "empty events"}
		}
	}

	if doc.Extensions.EventDelivery != nil {
		declaredTypes := map[string]struct{}{}
		for _, event := range pkg.Events {
			declaredTypes[event.Type] = struct{}{}
		}
		seen := map[string]struct{}{}
		for _, response := range doc.Extensions.EventDelivery.Responses {
			if strings.TrimSpace(response.EventType) == "" {
				return nil, &Error{Code: CodeEventInvalid, Detail: "eventType"}
			}
			if _, ok := declaredTypes[response.EventType]; !ok {
				return nil, &Error{Code: CodeEventInvalid, Detail: "undeclared response " + response.EventType}
			}
			if _, duplicate := seen[response.EventType]; duplicate {
				return nil, &Error{Code: CodeEventInvalid, Detail: "duplicate response " + response.EventType}
			}
			seen[response.EventType] = struct{}{}
			schemaPath, err := relativeFile(absoluteRoot, response.ResponseSchema, true)
			if err != nil {
				return nil, err
			}
			schema, err := loadSchema(schemaPath)
			if err != nil {
				return nil, err
			}
			declared := Response{EventType: response.EventType, ResponseSchema: response.ResponseSchema, Schema: schema}
			slotSeen := map[string]struct{}{}
			if response.Artifacts != nil {
				for _, artifact := range response.Artifacts.Output {
					if !slot.IsOutbound(artifact.Slot) {
						return nil, &Error{Code: CodeSlotInvalid, Detail: artifact.Slot}
					}
					expected, _ := slot.MediaType(artifact.Slot)
					if artifact.MediaType != expected {
						return nil, &Error{Code: CodeMediaTypeInvalid, Detail: artifact.Slot}
					}
					if artifact.Required == nil {
						return nil, &Error{Code: CodeEventInvalid, Detail: "required missing for " + artifact.Slot}
					}
					if _, duplicate := slotSeen[artifact.Slot]; duplicate {
						return nil, &Error{Code: CodeSlotInvalid, Detail: "duplicate " + artifact.Slot}
					}
					slotSeen[artifact.Slot] = struct{}{}
					declared.Output = append(declared.Output, Artifact{Slot: artifact.Slot, MediaType: artifact.MediaType, Required: *artifact.Required})
				}
			}
			pkg.Responses = append(pkg.Responses, declared)
		}
		if len(pkg.Responses) == 0 {
			return nil, &Error{Code: CodeEventInvalid, Detail: "empty responses"}
		}
	}

	if doc.Extensions.Tools != nil {
		if strings.TrimSpace(doc.Extensions.Tools.InterfacePackage) == "" {
			return nil, &Error{Code: CodeManifestInvalid, Detail: "tools.interfacePackage"}
		}
		pkg.ToolsInterfacePackage = doc.Extensions.Tools.InterfacePackage
	}
	return pkg, nil
}

func relativeFile(root, relative string, mustExist bool) (string, error) {
	if strings.TrimSpace(relative) == "" {
		return "", &Error{Code: CodePathMissing, Detail: "empty path"}
	}
	if strings.Contains(relative, ":") || filepath.IsAbs(relative) {
		return "", &Error{Code: CodePathEscape, Detail: relative}
	}
	cleaned := filepath.Clean(relative)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", &Error{Code: CodePathEscape, Detail: relative}
	}
	target := filepath.Join(root, cleaned)
	if !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return "", &Error{Code: CodePathEscape, Detail: relative}
	}
	if mustExist {
		info, err := os.Stat(target)
		if err != nil || !info.Mode().IsRegular() {
			return "", &Error{Code: CodePathMissing, Detail: relative}
		}
	}
	return target, nil
}

func loadSchema(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Code: CodeSchemaInvalid, Detail: filepath.Base(path), Err: err}
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, &Error{Code: CodeSchemaInvalid, Detail: filepath.Base(path), Err: err}
	}
	if len(schema) == 0 {
		return nil, &Error{Code: CodeSchemaInvalid, Detail: filepath.Base(path)}
	}
	return schema, nil
}
