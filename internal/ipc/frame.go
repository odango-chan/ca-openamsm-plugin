// Package ipc implements the connectionagent.io/plugin-ipc/v1alpha1 wire
// contract: strict UTF-8 NDJSON frame parsing/encoding and payload decoding.
//
// The package is protocol-only. It has no knowledge of Plugin semantics,
// Extensions, staging or Delivery; those live in higher packages.
package ipc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// APIVersion is the only supported process protocol version.
const APIVersion = "connectionagent.io/plugin-ipc/v1alpha1"

// Error is a protocol error. Code is a stable diagnostic token; Detail never
// carries business payload or raw bytes.
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

// Protocol error codes used by the Plugin and by tests.
const (
	CodeFrameEmpty            = "FRAME_EMPTY"
	CodeFrameInvalid          = "FRAME_INVALID"
	CodeFrameTooLarge         = "FRAME_TOO_LARGE"
	CodeFrameTrailingData     = "FRAME_TRAILING_DATA"
	CodeDuplicateJSONKey      = "JSON_DUPLICATE_KEY"
	CodeIPCVersionMismatch    = "IPC_VERSION_MISMATCH"
	CodeFrameFieldInvalid     = "FRAME_FIELD_INVALID"
	CodePayloadInvalid        = "PAYLOAD_INVALID"
	CodeFrameReadFailed       = "STDIN_READ_FAILED"
	CodeFrameWriteFailed      = "STDOUT_WRITE_FAILED"
	CodeDuplicateMessageID    = "DUPLICATE_MESSAGE_ID"
	CodeUnknownMessageType    = "UNKNOWN_MESSAGE_TYPE"
	CodeUnexpectedCorrelation = "UNEXPECTED_CORRELATION"
	CodeLifecycleViolation    = "LIFECYCLE_VIOLATION"
)

// Frame is one NDJSON IPC frame. Payload stays raw so that each consumer can
// apply its own closed field set.
type Frame struct {
	APIVersion    string          `json:"apiVersion"`
	MessageID     string          `json:"messageId"`
	CorrelationID string          `json:"correlationId,omitempty"`
	Type          string          `json:"type"`
	Payload       json.RawMessage `json:"payload"`
}

// Parse strictly decodes one frame line. Unknown envelope fields, duplicate
// JSON keys, a wrong apiVersion, missing identity or a non-object payload are
// rejected.
func Parse(line []byte) (Frame, error) {
	if len(bytes.TrimSpace(line)) == 0 {
		return Frame{}, &Error{Code: CodeFrameEmpty}
	}
	if err := checkDuplicateKeys(line); err != nil {
		return Frame{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	var frame Frame
	if err := decoder.Decode(&frame); err != nil {
		return Frame{}, &Error{Code: CodeFrameInvalid, Err: err}
	}
	if _, err := decoder.Token(); err != io.EOF {
		return Frame{}, &Error{Code: CodeFrameTrailingData}
	}
	if frame.APIVersion != APIVersion {
		return Frame{}, &Error{Code: CodeIPCVersionMismatch, Detail: frame.APIVersion}
	}
	if strings.TrimSpace(frame.MessageID) == "" {
		return Frame{}, &Error{Code: CodeFrameFieldInvalid, Detail: "messageId"}
	}
	if strings.TrimSpace(frame.Type) == "" {
		return Frame{}, &Error{Code: CodeFrameFieldInvalid, Detail: "type"}
	}
	payload := bytes.TrimSpace(frame.Payload)
	if len(payload) == 0 || payload[0] != '{' {
		return Frame{}, &Error{Code: CodeFrameFieldInvalid, Detail: "payload must be an object"}
	}
	return frame, nil
}

// DecodePayload strictly decodes a frame payload into T. The destination struct
// defines the closed field set; duplicate keys reject the frame.
func DecodePayload[T any](raw json.RawMessage) (T, error) {
	var value T
	if err := checkDuplicateKeys(raw); err != nil {
		return value, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, &Error{Code: CodePayloadInvalid, Err: err}
	}
	if _, err := decoder.Token(); err != io.EOF {
		return value, &Error{Code: CodeFrameTrailingData}
	}
	return value, nil
}

// checkDuplicateKeys rejects duplicate object member names at any depth. Go's
// encoding/json silently keeps the last value, which is unacceptable for an
// identity-bearing protocol.
type jsonScope struct {
	object    bool
	expectKey bool
	keys      map[string]struct{}
}

func checkDuplicateKeys(data []byte) error {
	var stack []*jsonScope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return &Error{Code: CodeFrameInvalid, Err: err}
		}
		switch value := token.(type) {
		case json.Delim:
			switch value {
			case '{':
				stack = append(stack, &jsonScope{object: true, expectKey: true, keys: map[string]struct{}{}})
			case '[':
				stack = append(stack, &jsonScope{})
			case '}', ']':
				if len(stack) == 0 {
					return &Error{Code: CodeFrameInvalid, Detail: "unbalanced delimiter"}
				}
				stack = stack[:len(stack)-1]
				markValueDone(stack)
			}
		case string:
			if len(stack) > 0 && stack[len(stack)-1].object && stack[len(stack)-1].expectKey {
				top := stack[len(stack)-1]
				if _, duplicate := top.keys[value]; duplicate {
					return &Error{Code: CodeDuplicateJSONKey, Detail: value}
				}
				top.keys[value] = struct{}{}
				top.expectKey = false
			} else {
				markValueDone(stack)
			}
		default:
			markValueDone(stack)
		}
	}
}

func markValueDone(stack []*jsonScope) {
	if len(stack) == 0 {
		return
	}
	if top := stack[len(stack)-1]; top.object {
		top.expectKey = true
	}
}

// NewErrorf builds a protocol error with a formatted detail.
func NewErrorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}
