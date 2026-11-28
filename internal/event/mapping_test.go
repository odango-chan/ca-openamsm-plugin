package event_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/odango-chan/ca-openamsm-plugin/internal/event"
	"github.com/odango-chan/ca-openamsm-plugin/internal/slot"
	"github.com/odango-chan/ca-openamsm-plugin/internal/testsupport"
)

func binaryPart(contentID, mediaType string, payload []byte) *event.BinaryPart {
	return &event.BinaryPart{ContentID: contentID, MediaType: mediaType, Bytes: payload}
}

func partSlots(parts []event.Part) []string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		values = append(values, part.Slot)
	}
	return values
}

func payloadFields(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("payload fields: %v", err)
	}
	return fields
}

func TestAC00904CreatePositive(t *testing.T) {
	input := event.Input{
		Operation: event.OperationCreate,
		Request:   testsupport.CreateJSON(),
		N1:        binaryPart("n1SmMsg", slot.MediaTypeNAS, testsupport.N1Modify),
		N2:        binaryPart("n2SmInfo", slot.MediaTypeNGAP, testsupport.N2SetupResponse),
		N2Type:    "PDU_RES_SETUP_REQ",
	}
	normalized, err := event.Normalize(input, nil)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized.Type != event.TypeCreate {
		t.Fatalf("eventType = %q", normalized.Type)
	}
	fields := payloadFields(t, normalized.Payload)
	var operationID string
	if err := json.Unmarshal(fields["operationId"], &operationID); err != nil || operationID != event.OperationCreate {
		t.Fatalf("operationId = %q err=%v", operationID, err)
	}
	if _, present := fields["discriminator"]; present {
		t.Fatalf("Create must not carry a discriminator")
	}
	if string(fields["request"]) != string(testsupport.CreateJSON()) {
		t.Fatalf("request was rewritten")
	}
	expectSlots(t, partSlots(normalized.Parts), []string{slot.N1Request, slot.N2Request})
	if string(normalized.Parts[0].Bytes) != string(testsupport.N1Modify) {
		t.Fatalf("N1 bytes not preserved")
	}
	if normalized.Parts[0].ContentID != "n1SmMsg" || normalized.Parts[0].MediaType != slot.MediaTypeNAS {
		t.Fatalf("part metadata = %+v", normalized.Parts[0])
	}
}

func expectSlots(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slots = %v, want %v", got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("slots = %v, want %v", got, want)
		}
	}
}

func TestAC00904CreateWithN2Ext1(t *testing.T) {
	request := []byte(`{"supi":"imsi-1","n1SmMsg":{"contentId":"n1SmMsg"},"n2SmInfo":{"contentId":"n2SmInfo"},"n2SmInfoType":"PDU_RES_SETUP_REQ","n2SmInfoExt1":{"contentId":"n2SmInfoExt1"},"n2SmInfoTypeExt1":"PDU_RES_SETUP_FAIL"}`)
	normalized, err := event.Normalize(event.Input{
		Operation:  event.OperationCreate,
		Request:    request,
		N1:         binaryPart("n1SmMsg", slot.MediaTypeNAS, testsupport.N1Modify),
		N2:         binaryPart("n2SmInfo", slot.MediaTypeNGAP, testsupport.N2SetupResponse),
		N2Type:     "PDU_RES_SETUP_REQ",
		N2Ext1:     binaryPart("n2SmInfoExt1", slot.MediaTypeNGAP, testsupport.N2Ext1Response),
		N2Ext1Type: "PDU_RES_SETUP_FAIL",
	}, nil)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	expectSlots(t, partSlots(normalized.Parts), []string{slot.N1Request, slot.N2Request, slot.N2Ext1Request})
	if string(normalized.Parts[2].Bytes) != string(testsupport.N2Ext1Response) {
		t.Fatalf("N2 Ext1 bytes not preserved")
	}
	if normalized.Parts[2].MediaType != slot.MediaTypeNGAP {
		t.Fatalf("Ext1 media type = %q", normalized.Parts[2].MediaType)
	}
}

func TestAC00904UpdateModifyAndRelease(t *testing.T) {
	cases := []struct {
		name     string
		payload  []byte
		expected event.N1Class
	}{
		{name: "modify-0xC9", payload: testsupport.N1Modify, expected: event.N1Modify},
		{name: "release-0xD1", payload: testsupport.N1Release, expected: event.N1Release},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			normalized, err := event.Normalize(event.Input{
				Operation:    event.OperationUpdate,
				SmContextRef: "ref-1",
				Request:      testsupport.UpdateJSONModify(),
				N1:           binaryPart("n1SmMsg", slot.MediaTypeNAS, testCase.payload),
				N2:           binaryPart("n2SmInfo", slot.MediaTypeNGAP, testsupport.N2ModResponse),
				N2Type:       "PDU_RES_MOD_REQ",
			}, event.TestDoubleClassifier{})
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if normalized.Type != event.TypeUpdate {
				t.Fatalf("eventType = %q", normalized.Type)
			}
			fields := payloadFields(t, normalized.Payload)
			var discriminator struct {
				N1MessageType string `json:"n1MessageType"`
				N2SmInfoType  string `json:"n2SmInfoType"`
			}
			if err := json.Unmarshal(fields["discriminator"], &discriminator); err != nil {
				t.Fatalf("discriminator: %v", err)
			}
			if discriminator.N1MessageType != string(testCase.expected) {
				t.Fatalf("n1MessageType = %q", discriminator.N1MessageType)
			}
			if discriminator.N2SmInfoType != "" {
				t.Fatalf("Update discriminator must not carry n2SmInfoType")
			}
			var smContextRef string
			if err := json.Unmarshal(fields["smContextRef"], &smContextRef); err != nil || smContextRef != "ref-1" {
				t.Fatalf("smContextRef = %q err=%v", smContextRef, err)
			}
			expectSlots(t, partSlots(normalized.Parts), []string{slot.N1Request, slot.N2Request})
		})
	}
}

func TestAC00904UpdateWithOptionalN2AndExt1(t *testing.T) {
	request := []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"},"n2SmInfo":{"contentId":"n2SmInfo"},"n2SmInfoType":"PDU_RES_MOD_REQ","n2SmInfoExt1":{"contentId":"n2SmInfoExt1"},"n2SmInfoTypeExt1":"PDU_RES_MOD_CFM"}`)
	normalized, err := event.Normalize(event.Input{
		Operation:    event.OperationUpdate,
		SmContextRef: "ref-2",
		Request:      request,
		N1:           binaryPart("n1SmMsg", slot.MediaTypeNAS, testsupport.N1Modify),
		N2:           binaryPart("n2SmInfo", slot.MediaTypeNGAP, testsupport.N2ModResponse),
		N2Type:       "PDU_RES_MOD_REQ",
		N2Ext1:       binaryPart("n2SmInfoExt1", slot.MediaTypeNGAP, testsupport.N2Ext1Response),
		N2Ext1Type:   "PDU_RES_MOD_CFM",
	}, event.TestDoubleClassifier{})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	expectSlots(t, partSlots(normalized.Parts), []string{slot.N1Request, slot.N2Request, slot.N2Ext1Request})
}

func TestAC00904ContinuationN1ReleaseComplete(t *testing.T) {
	normalized, err := event.Normalize(event.Input{
		Operation:    event.OperationUpdate,
		SmContextRef: "ref-1",
		Request:      testsupport.UpdateContinuationJSON(),
		N1:           binaryPart("n1SmMsg", slot.MediaTypeNAS, testsupport.N1ReleaseComplete),
	}, event.TestDoubleClassifier{})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized.Type != event.TypeContinuation {
		t.Fatalf("eventType = %q", normalized.Type)
	}
	expectSlots(t, partSlots(normalized.Parts), []string{slot.N1Confirmation})
	fields := payloadFields(t, normalized.Payload)
	var discriminator struct {
		N1MessageType string `json:"n1MessageType"`
	}
	if err := json.Unmarshal(fields["discriminator"], &discriminator); err != nil || discriminator.N1MessageType != "0xD4" {
		t.Fatalf("discriminator = %+v err=%v", discriminator, err)
	}
}

func TestAC00904ContinuationDualPath(t *testing.T) {
	request := []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"},"n2SmInfo":{"contentId":"n2SmInfo"},"n2SmInfoType":"PDU_RES_REL_RSP"}`)
	normalized, err := event.Normalize(event.Input{
		Operation:    event.OperationUpdate,
		SmContextRef: "ref-1",
		Request:      request,
		N1:           binaryPart("n1SmMsg", slot.MediaTypeNAS, testsupport.N1ReleaseComplete),
		N2:           binaryPart("n2SmInfo", slot.MediaTypeNGAP, testsupport.N2RelResponse),
		N2Type:       "PDU_RES_REL_RSP",
	}, event.TestDoubleClassifier{})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized.Type != event.TypeContinuation {
		t.Fatalf("eventType = %q", normalized.Type)
	}
	expectSlots(t, partSlots(normalized.Parts), []string{slot.N1Confirmation, slot.N2Confirmation})
	fields := payloadFields(t, normalized.Payload)
	var discriminator struct {
		N1MessageType string `json:"n1MessageType"`
		N2SmInfoType  string `json:"n2SmInfoType"`
	}
	if err := json.Unmarshal(fields["discriminator"], &discriminator); err != nil {
		t.Fatalf("discriminator: %v", err)
	}
	if discriminator.N1MessageType != "0xD4" || discriminator.N2SmInfoType != "PDU_RES_REL_RSP" {
		t.Fatalf("discriminator = %+v", discriminator)
	}
}

func TestAC00904ContinuationN2Only(t *testing.T) {
	for _, n2Type := range []string{"PDU_RES_SETUP_RSP", "PDU_RES_MOD_RSP", "PDU_RES_REL_RSP"} {
		t.Run(n2Type, func(t *testing.T) {
			normalized, err := event.Normalize(event.Input{
				Operation:    event.OperationUpdate,
				SmContextRef: "ref-1",
				Request:      testsupport.UpdateN2ContinuationJSON(n2Type),
				N2:           binaryPart("n2SmInfo", slot.MediaTypeNGAP, testsupport.N2SetupResponse),
				N2Type:       n2Type,
			}, event.TestDoubleClassifier{})
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if normalized.Type != event.TypeContinuation {
				t.Fatalf("eventType = %q", normalized.Type)
			}
			expectSlots(t, partSlots(normalized.Parts), []string{slot.N2Confirmation})
		})
	}
}

func TestAC00904RejectConflictingDiscriminator(t *testing.T) {
	cases := map[string]event.Input{
		"new-and-completion": {
			Operation: event.OperationUpdate, SmContextRef: "ref-1", Request: testsupport.UpdateJSONModify(),
			N1: binaryPart("n1SmMsg", slot.MediaTypeNAS, testsupport.N1Modify),
			N2: binaryPart("n2SmInfo", slot.MediaTypeNGAP, testsupport.N2SetupResponse), N2Type: "PDU_RES_SETUP_RSP",
		},
		"release-complete-with-request": {
			Operation: event.OperationUpdate, SmContextRef: "ref-1", Request: testsupport.UpdateJSONModify(),
			N1: binaryPart("n1SmMsg", slot.MediaTypeNAS, testsupport.N1ReleaseComplete),
			N2: binaryPart("n2SmInfo", slot.MediaTypeNGAP, testsupport.N2ModResponse), N2Type: "PDU_RES_MOD_REQ",
		},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := event.Normalize(input, event.TestDoubleClassifier{})
			var normalizationError *event.Error
			if !errors.As(err, &normalizationError) || normalizationError.Code != event.CodeConflictingDiscriminator {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestAC00904RejectMalformedAndUnsupportedN1(t *testing.T) {
	cases := map[string]struct {
		payload []byte
		code    string
	}{
		"truncated":    {payload: []byte{0x2e, 0x05, 0x01}, code: event.CodeN1Malformed},
		"wrong-epd":    {payload: []byte{0x2f, 0x05, 0x01, 0xc9}, code: event.CodeN1Malformed},
		"unknown-type": {payload: []byte{0x2e, 0x05, 0x01, 0xca}, code: event.CodeN1Unsupported},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := event.Normalize(event.Input{
				Operation: event.OperationUpdate, SmContextRef: "ref-1", Request: testsupport.UpdateJSONModify(),
				N1: binaryPart("n1SmMsg", slot.MediaTypeNAS, testCase.payload),
			}, event.TestDoubleClassifier{})
			var normalizationError *event.Error
			if !errors.As(err, &normalizationError) || normalizationError.Code != testCase.code {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestAC00904RejectMissingN1(t *testing.T) {
	if _, err := event.Normalize(event.Input{Operation: event.OperationCreate, Request: testsupport.CreateJSON()}, nil); err == nil {
		t.Fatalf("Create without N1 must fail")
	}
	if _, err := event.Normalize(event.Input{
		Operation: event.OperationUpdate, SmContextRef: "ref-1", Request: testsupport.UpdateJSONModify(),
		N2: binaryPart("n2SmInfo", slot.MediaTypeNGAP, testsupport.N2ModResponse), N2Type: "PDU_RES_MOD_REQ",
	}, event.TestDoubleClassifier{}); err == nil {
		t.Fatalf("Update without N1 must fail")
	}
}

func TestAC00904RejectN2Integrity(t *testing.T) {
	cases := map[string]struct {
		input event.Input
		code  string
	}{
		"part-without-type": {
			input: event.Input{Operation: event.OperationUpdate, SmContextRef: "r", Request: testsupport.UpdateJSONModify(),
				N1: binaryPart("n1", slot.MediaTypeNAS, testsupport.N1Modify), N2: binaryPart("n2", slot.MediaTypeNGAP, testsupport.N2ModResponse)},
			code: event.CodeN2TypeMissing,
		},
		"type-without-part": {
			input: event.Input{Operation: event.OperationUpdate, SmContextRef: "r", Request: testsupport.UpdateJSONModify(),
				N1: binaryPart("n1", slot.MediaTypeNAS, testsupport.N1Modify), N2Type: "PDU_RES_MOD_REQ"},
			code: event.CodeN2TypeWithoutPart,
		},
		"unknown-type": {
			input: event.Input{Operation: event.OperationUpdate, SmContextRef: "r", Request: testsupport.UpdateJSONModify(),
				N1: binaryPart("n1", slot.MediaTypeNAS, testsupport.N1Modify), N2: binaryPart("n2", slot.MediaTypeNGAP, testsupport.N2ModResponse), N2Type: "BOGUS_N2_TYPE"},
			code: event.CodeN2TypeUnknown,
		},
		"ext1-type-without-part": {
			input: event.Input{Operation: event.OperationUpdate, SmContextRef: "r", Request: testsupport.UpdateJSONModify(),
				N1: binaryPart("n1", slot.MediaTypeNAS, testsupport.N1Modify), N2Ext1Type: "PDU_RES_MOD_CFM"},
			code: event.CodeN2Ext1TypeWithoutPart,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := event.Normalize(testCase.input, event.TestDoubleClassifier{})
			var normalizationError *event.Error
			if !errors.As(err, &normalizationError) || normalizationError.Code != testCase.code {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestAC00904RejectExt1InContinuationNotRun(t *testing.T) {
	// The continuation contract has no frozen Ext1 slot. This sub-item is
	// explicitly NOT_RUN (P2-01), and the ingress fails closed rather than
	// guessing a slot or silently dropping bytes.
	cases := map[string]event.Input{
		"n1-completion": {
			Operation: event.OperationUpdate, SmContextRef: "r", Request: testsupport.UpdateContinuationJSON(),
			N1:     binaryPart("n1", slot.MediaTypeNAS, testsupport.N1ReleaseComplete),
			N2Ext1: binaryPart("ext1", slot.MediaTypeNGAP, testsupport.N2Ext1Response), N2Ext1Type: "PDU_RES_SETUP_FAIL",
		},
		"n2-only-completion": {
			Operation: event.OperationUpdate, SmContextRef: "r", Request: testsupport.UpdateN2ContinuationJSON("PDU_RES_SETUP_RSP"),
			N2: binaryPart("n2", slot.MediaTypeNGAP, testsupport.N2SetupResponse), N2Type: "PDU_RES_SETUP_RSP",
			N2Ext1: binaryPart("ext1", slot.MediaTypeNGAP, testsupport.N2Ext1Response), N2Ext1Type: "PDU_RES_SETUP_FAIL",
		},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := event.Normalize(input, event.TestDoubleClassifier{})
			var normalizationError *event.Error
			if !errors.As(err, &normalizationError) || normalizationError.Code != event.CodeN2Ext1ContinuationUnspec {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestAC00904ProductionClassifierBlocked(t *testing.T) {
	_, err := event.Normalize(event.Input{
		Operation: event.OperationUpdate, SmContextRef: "ref-1", Request: testsupport.UpdateJSONModify(),
		N1: binaryPart("n1SmMsg", slot.MediaTypeNAS, testsupport.N1Modify),
	}, event.BlockedClassifier{})
	var normalizationError *event.Error
	if !errors.As(err, &normalizationError) || normalizationError.Code != event.CodeT0Blocked {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, event.ErrBlockedExternalInput) {
		t.Fatalf("must wrap ErrBlockedExternalInput")
	}
}

func TestAC00904PayloadHasNoHostOrSkillIdentity(t *testing.T) {
	normalized, err := event.Normalize(event.Input{
		Operation: event.OperationCreate, Request: testsupport.CreateJSON(),
		N1: binaryPart("n1SmMsg", slot.MediaTypeNAS, testsupport.N1Modify),
	}, nil)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	fields := payloadFields(t, normalized.Payload)
	allowed := map[string]bool{"operationId": true, "smContextRef": true, "request": true, "discriminator": true}
	for key := range fields {
		if !allowed[key] {
			t.Fatalf("payload leaked field %q", key)
		}
	}
	for _, forbidden := range []string{"runId", "skillId", "responseObligationId", "deliveryId", "correlationId", "artifactRef", "capabilityGrant"} {
		if _, present := fields[forbidden]; present {
			t.Fatalf("payload leaked %q", forbidden)
		}
	}
}

func TestAC00904RequestPreservedByteIdentical(t *testing.T) {
	request := []byte(`{"requestType":"INITIAL_REQUEST","pduSessionId":7,"unknownFutureField":{"nested":[1,2,3]},"n1SmMsg":{"contentId":"n1"}}`)
	normalized, err := event.Normalize(event.Input{
		Operation: event.OperationCreate, Request: request,
		N1: binaryPart("n1", slot.MediaTypeNAS, testsupport.N1Modify),
	}, nil)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	fields := payloadFields(t, normalized.Payload)
	if string(fields["request"]) != string(request) {
		t.Fatalf("request rewritten: %s", fields["request"])
	}
}

func TestAC00904N1MessageTypeAtOffsetThree(t *testing.T) {
	classifier := event.TestDoubleClassifier{}
	cases := map[string]event.N1Class{
		"0xC9": event.N1Modify,
		"0xD1": event.N1Release,
		"0xD4": event.N1ReleaseComplete,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			payload := []byte{0x2e, 0xc9, 0xd1, byte(0)}
			switch name {
			case "0xC9":
				payload[3] = 0xc9
			case "0xD1":
				payload[3] = 0xd1
			case "0xD4":
				payload[3] = 0xd4
			}
			got, err := classifier.ClassifyN1(payload)
			if err != nil || got != want {
				t.Fatalf("classify = %q err=%v", got, err)
			}
		})
	}
	if _, err := classifier.ClassifyN1([]byte{0x2e, 0xc9, 0xd1, 0x01}); !errors.Is(err, event.ErrUnsupportedN1) {
		t.Fatalf("unsupported message must fail: %v", err)
	}
}

func TestAC00904FrozenN2Enumeration(t *testing.T) {
	if got := len(event.N2Types()); got != 26 {
		t.Fatalf("N2SmInfoType count = %d, want 26 from TS 29.502 V19.5.0", got)
	}
	for _, continuation := range []string{"PDU_RES_SETUP_RSP", "PDU_RES_MOD_RSP", "PDU_RES_REL_RSP"} {
		if !event.IsContinuationN2Type(continuation) {
			t.Fatalf("%s must classify as continuation", continuation)
		}
	}
	for _, request := range []string{"PDU_RES_SETUP_REQ", "PDU_RES_MOD_REQ", "PDU_RES_REL_CMD"} {
		if event.IsContinuationN2Type(request) {
			t.Fatalf("%s must not classify as continuation", request)
		}
	}
}
