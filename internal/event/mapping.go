package event

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/odango-chan/ca-openamsm-plugin/internal/slot"
)

// Event types frozen by F06 §4.1.
const (
	TypeCreate       = "nsmf.create-sm-context"
	TypeUpdate       = "nsmf.update-sm-context"
	TypeContinuation = "nsmf.update-sm-context.continuation"

	OperationCreate = "PostSmContexts"
	OperationUpdate = "UpdateSmContext"
)

// Error codes produced by Event normalization.
const (
	CodeRequestInvalid           = "REQUEST_INVALID"
	CodeMissingRequiredSlot      = "MISSING_REQUIRED_SLOT"
	CodeN1Malformed              = "N1_MALFORMED"
	CodeN1Unsupported            = "N1_UNSUPPORTED"
	CodeT0Blocked                = "T0_BLOCKED_EXTERNAL_INPUT"
	CodeN2TypeMissing            = "N2_TYPE_MISSING"
	CodeN2TypeUnknown            = "N2_TYPE_UNKNOWN"
	CodeN2TypeWithoutPart        = "N2_TYPE_WITHOUT_PART"
	CodeN2Ext1TypeWithoutPart    = "N2_EXT1_TYPE_WITHOUT_PART"
	CodeConflictingDiscriminator = "CONFLICTING_DISCRIMINATOR"
	CodeN2Ext1ContinuationUnspec = "N2_EXT1_CONTINUATION_UNSPECIFIED"
)

// Error is a normalization failure with a stable code.
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

// BinaryPart is one raw SBI binary part resolved from multipart/related.
type BinaryPart struct {
	ContentID string
	MediaType string
	Bytes     []byte
}

// Input is the parsed SBI request before classification.
type Input struct {
	Operation    string
	SmContextRef string
	Request      json.RawMessage
	N1           *BinaryPart
	N2           *BinaryPart
	N2Ext1       *BinaryPart
	N2Type       string
	N2Ext1Type   string
}

// Part is one normalized Event artifact to stage.
type Part struct {
	Slot      string
	MediaType string
	ContentID string
	Bytes     []byte
}

// Event is one normalized Event.
type Event struct {
	Type    string
	Payload json.RawMessage
	Parts   []Part
}

type discriminator struct {
	N1MessageType string `json:"n1MessageType,omitempty"`
	N2SmInfoType  string `json:"n2SmInfoType,omitempty"`
}

type eventPayload struct {
	OperationID   string          `json:"operationId"`
	SmContextRef  string          `json:"smContextRef,omitempty"`
	Request       json.RawMessage `json:"request"`
	Discriminator *discriminator  `json:"discriminator,omitempty"`
}

// Normalize maps one parsed SBI request to the frozen Event contract. It never
// derives business fields, Skill identity, Run identity or artifact references.
func Normalize(input Input, classifier Classifier) (Event, error) {
	request := bytes.TrimSpace(input.Request)
	if len(request) == 0 || request[0] != '{' || !json.Valid(request) {
		return Event{}, &Error{Code: CodeRequestInvalid}
	}
	switch input.Operation {
	case OperationCreate:
		return normalizeCreate(input, request)
	case OperationUpdate:
		return normalizeUpdate(input, request, classifier)
	default:
		return Event{}, &Error{Code: CodeRequestInvalid, Detail: "operation"}
	}
}

func normalizeCreate(input Input, request json.RawMessage) (Event, error) {
	if input.N1 == nil {
		return Event{}, &Error{Code: CodeMissingRequiredSlot, Detail: slot.N1Request}
	}
	if input.N2 != nil && input.N2Type != "" && !IsKnownN2Type(input.N2Type) {
		return Event{}, &Error{Code: CodeN2TypeUnknown, Detail: input.N2Type}
	}
	if input.N2 == nil && input.N2Type != "" {
		return Event{}, &Error{Code: CodeN2TypeWithoutPart, Detail: input.N2Type}
	}
	if input.N2Ext1 == nil && input.N2Ext1Type != "" {
		return Event{}, &Error{Code: CodeN2Ext1TypeWithoutPart, Detail: input.N2Ext1Type}
	}
	if input.N2Ext1Type != "" && !IsKnownN2Type(input.N2Ext1Type) {
		return Event{}, &Error{Code: CodeN2TypeUnknown, Detail: input.N2Ext1Type}
	}

	parts := []Part{partFor(slot.N1Request, input.N1)}
	if input.N2 != nil {
		parts = append(parts, partFor(slot.N2Request, input.N2))
	}
	if input.N2Ext1 != nil {
		parts = append(parts, partFor(slot.N2Ext1Request, input.N2Ext1))
	}
	payload, err := marshalPayload(eventPayload{
		OperationID: OperationCreate,
		Request:     request,
	})
	if err != nil {
		return Event{}, err
	}
	return Event{Type: TypeCreate, Payload: payload, Parts: parts}, nil
}

func normalizeUpdate(input Input, request json.RawMessage, classifier Classifier) (Event, error) {
	if classifier == nil {
		return Event{}, &Error{Code: CodeT0Blocked, Detail: "classifier not configured"}
	}
	// Part/type integrity is checked before classification so that a mismatched
	// pair fails without invoking the T0 seam.
	if input.N2 != nil && input.N2Type == "" {
		return Event{}, &Error{Code: CodeN2TypeMissing}
	}
	if input.N2 == nil && input.N2Type != "" {
		return Event{}, &Error{Code: CodeN2TypeWithoutPart, Detail: input.N2Type}
	}
	if input.N2Type != "" && !IsKnownN2Type(input.N2Type) {
		return Event{}, &Error{Code: CodeN2TypeUnknown, Detail: input.N2Type}
	}
	if input.N2Ext1 == nil && input.N2Ext1Type != "" {
		return Event{}, &Error{Code: CodeN2Ext1TypeWithoutPart, Detail: input.N2Ext1Type}
	}
	if input.N2Ext1Type != "" && !IsKnownN2Type(input.N2Ext1Type) {
		return Event{}, &Error{Code: CodeN2TypeUnknown, Detail: input.N2Ext1Type}
	}
	continuationN2 := IsContinuationN2Type(input.N2Type)

	if input.N1 == nil {
		// N2-only continuation is a frozen F06 §5.1 case. A new Modify or Release
		// always carries N1.
		if continuationN2 {
			if input.N2Ext1 != nil {
				return Event{}, &Error{Code: CodeN2Ext1ContinuationUnspec, Detail: "no frozen continuation Ext1 slot"}
			}
			payload, err := marshalPayload(eventPayload{
				OperationID:   OperationUpdate,
				SmContextRef:  input.SmContextRef,
				Request:       request,
				Discriminator: &discriminator{N2SmInfoType: input.N2Type},
			})
			if err != nil {
				return Event{}, err
			}
			return Event{
				Type:    TypeContinuation,
				Payload: payload,
				Parts:   []Part{partFor(slot.N2Confirmation, input.N2)},
			}, nil
		}
		return Event{}, &Error{Code: CodeMissingRequiredSlot, Detail: slot.N1Request}
	}

	class, err := classifier.ClassifyN1(input.N1.Bytes)
	if err != nil {
		switch {
		case errors.Is(err, ErrBlockedExternalInput):
			return Event{}, &Error{Code: CodeT0Blocked, Detail: err.Error(), Err: err}
		case errors.Is(err, ErrMalformedN1):
			return Event{}, &Error{Code: CodeN1Malformed, Detail: err.Error(), Err: err}
		case errors.Is(err, ErrUnsupportedN1):
			return Event{}, &Error{Code: CodeN1Unsupported, Detail: err.Error(), Err: err}
		default:
			return Event{}, &Error{Code: CodeT0Blocked, Detail: err.Error(), Err: err}
		}
	}

	switch class {
	case N1ReleaseComplete:
		if input.N2 != nil && !continuationN2 {
			return Event{}, &Error{Code: CodeConflictingDiscriminator, Detail: "0xD4+" + input.N2Type}
		}
		if input.N2Ext1 != nil {
			return Event{}, &Error{Code: CodeN2Ext1ContinuationUnspec, Detail: "no frozen continuation Ext1 slot"}
		}
		parts := []Part{partFor(slot.N1Confirmation, input.N1)}
		value := &discriminator{N1MessageType: string(N1ReleaseComplete)}
		if input.N2 != nil {
			parts = append(parts, partFor(slot.N2Confirmation, input.N2))
			value.N2SmInfoType = input.N2Type
		}
		payload, err := marshalPayload(eventPayload{
			OperationID:   OperationUpdate,
			SmContextRef:  input.SmContextRef,
			Request:       request,
			Discriminator: value,
		})
		if err != nil {
			return Event{}, err
		}
		return Event{Type: TypeContinuation, Payload: payload, Parts: parts}, nil
	case N1Modify, N1Release:
		if input.N2 != nil && continuationN2 {
			return Event{}, &Error{Code: CodeConflictingDiscriminator, Detail: string(class) + "+" + input.N2Type}
		}
		parts := []Part{partFor(slot.N1Request, input.N1)}
		if input.N2 != nil {
			parts = append(parts, partFor(slot.N2Request, input.N2))
		}
		if input.N2Ext1 != nil {
			parts = append(parts, partFor(slot.N2Ext1Request, input.N2Ext1))
		}
		payload, err := marshalPayload(eventPayload{
			OperationID:   OperationUpdate,
			SmContextRef:  input.SmContextRef,
			Request:       request,
			Discriminator: &discriminator{N1MessageType: string(class)},
		})
		if err != nil {
			return Event{}, err
		}
		return Event{Type: TypeUpdate, Payload: payload, Parts: parts}, nil
	default:
		return Event{}, &Error{Code: CodeN1Unsupported, Detail: string(class)}
	}
}

func partFor(slotName string, part *BinaryPart) Part {
	mediaType, _ := slot.MediaType(slotName)
	return Part{Slot: slotName, MediaType: mediaType, ContentID: part.ContentID, Bytes: part.Bytes}
}

func marshalPayload(value eventPayload) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, &Error{Code: CodeRequestInvalid, Detail: fmt.Sprintf("encode payload: %v", err)}
	}
	return encoded, nil
}
