package ipc

import (
	"encoding/json"
	"io"
	"sync"
)

// Writer serializes concurrent frame writes to stdout. stdout carries protocol
// frames only; diagnostics must go to stderr.
type Writer struct {
	mu sync.Mutex
	w  io.Writer
}

func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }

// Send encodes one frame. correlationID is omitted when empty.
func (w *Writer) Send(messageID, correlationID, messageType string, payload any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return &Error{Code: CodeFrameWriteFailed, Err: err}
	}
	line, err := json.Marshal(Frame{
		APIVersion:    APIVersion,
		MessageID:     messageID,
		CorrelationID: correlationID,
		Type:          messageType,
		Payload:       payloadJSON,
	})
	if err != nil {
		return &Error{Code: CodeFrameWriteFailed, Err: err}
	}
	line = append(line, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.w.Write(line); err != nil {
		return &Error{Code: CodeFrameWriteFailed, Err: err}
	}
	return nil
}
