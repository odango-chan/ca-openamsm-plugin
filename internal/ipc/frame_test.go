package ipc_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/odango-chan/ca-openamsm-plugin/internal/ipc"
)

const apiVersion = "connectionagent.io/plugin-ipc/v1alpha1"

func validFrame(t *testing.T, overrides map[string]any) []byte {
	t.Helper()
	frame := map[string]any{
		"apiVersion": apiVersion,
		"messageId":  "m1",
		"type":       "ingress.ack",
		"payload":    map[string]any{"eventId": "evt1", "disposition": "accepted"},
	}
	for key, value := range overrides {
		frame[key] = value
	}
	encoded, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return encoded
}

func TestAC00901FrameRoundTripAndCorrelation(t *testing.T) {
	var buffer bytes.Buffer
	writer := ipc.NewWriter(&buffer)
	if err := writer.Send("m1", "req-1", "ingress.event", map[string]any{
		"eventId":   "evt1",
		"artifacts": []any{},
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	reader := ipc.NewReader(&buffer, 64*1024)
	frame, err := reader.Next()
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if frame.APIVersion != ipc.APIVersion || frame.MessageID != "m1" || frame.CorrelationID != "req-1" || frame.Type != "ingress.event" {
		t.Fatalf("envelope = %+v", frame)
	}
	payload, err := ipc.DecodePayload[struct {
		EventID   string `json:"eventId"`
		Artifacts []any  `json:"artifacts"`
	}](frame.Payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.EventID != "evt1" {
		t.Fatalf("eventId = %q", payload.EventID)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestAC00901RejectWrongAPIVersion(t *testing.T) {
	line := validFrame(t, map[string]any{"apiVersion": "connectionagent.io/plugin-ipc/v2"})
	_, err := ipc.Parse(line)
	var protocolError *ipc.Error
	if !errors.As(err, &protocolError) || protocolError.Code != ipc.CodeIPCVersionMismatch {
		t.Fatalf("err = %v", err)
	}
}

func TestAC00901RejectUnknownEnvelopeField(t *testing.T) {
	line := validFrame(t, map[string]any{"runId": "run-1"})
	_, err := ipc.Parse(line)
	var protocolError *ipc.Error
	if !errors.As(err, &protocolError) || protocolError.Code != ipc.CodeFrameInvalid {
		t.Fatalf("err = %v", err)
	}
}

func TestAC00901RejectDuplicateJSONKey(t *testing.T) {
	line := []byte(`{"apiVersion":"` + apiVersion + `","messageId":"m1","type":"ingress.ack","payload":{"eventId":"evt1","eventId":"evt2","disposition":"accepted"}}`)
	_, err := ipc.Parse(line)
	var protocolError *ipc.Error
	if !errors.As(err, &protocolError) || protocolError.Code != ipc.CodeDuplicateJSONKey {
		t.Fatalf("err = %v", err)
	}
}

func TestAC00901RejectInvalidFrames(t *testing.T) {
	cases := map[string][]byte{
		"empty":           []byte("   "),
		"missing id":      []byte(`{"apiVersion":"` + apiVersion + `","type":"ingress.ack","payload":{}}`),
		"missing type":    []byte(`{"apiVersion":"` + apiVersion + `","messageId":"m1","payload":{}}`),
		"payload array":   []byte(`{"apiVersion":"` + apiVersion + `","messageId":"m1","type":"ingress.ack","payload":[]}`),
		"payload missing": []byte(`{"apiVersion":"` + apiVersion + `","messageId":"m1","type":"ingress.ack"}`),
		"trailing":        append(validFrame(t, nil), []byte(`{"extra":true}`)...),
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ipc.Parse(line); err == nil {
				t.Fatalf("expected rejection")
			}
		})
	}
}

func TestAC00901RejectOversizedFrame(t *testing.T) {
	large := strings.Repeat("a", 2048)
	line := []byte(`{"apiVersion":"` + apiVersion + `","messageId":"m1","type":"ingress.ack","payload":{"x":"` + large + `"}}`)
	reader := ipc.NewReader(bytes.NewReader(append(line, '\n')), 1024)
	_, err := reader.Next()
	var protocolError *ipc.Error
	if !errors.As(err, &protocolError) || protocolError.Code != ipc.CodeFrameTooLarge {
		t.Fatalf("err = %v", err)
	}
}

func TestAC00901PayloadStrictAllowlist(t *testing.T) {
	good := map[string]any{"eventId": "evt1", "disposition": "accepted"}
	type ack struct {
		EventID     string `json:"eventId"`
		Disposition string `json:"disposition"`
	}
	encoded, _ := json.Marshal(good)
	if _, err := ipc.DecodePayload[ack](encoded); err != nil {
		t.Fatalf("good payload: %v", err)
	}
	for _, forbidden := range []string{"runId", "skillId", "artifactRef", "responseObligationId", "deliveryId"} {
		injected := map[string]any{"eventId": "evt1", "disposition": "accepted"}
		injected[forbidden] = "leak"
		encoded, _ := json.Marshal(injected)
		if _, err := ipc.DecodePayload[ack](encoded); err == nil {
			t.Fatalf("payload with %s must be rejected", forbidden)
		}
	}
}

func TestAC00901ConcurrentWriterSerializesFrames(t *testing.T) {
	var buffer bytes.Buffer
	writer := ipc.NewWriter(&buffer)
	const writers = 32
	var wait sync.WaitGroup
	wait.Add(writers)
	for index := 0; index < writers; index++ {
		go func(index int) {
			defer wait.Done()
			for repeat := 0; repeat < 8; repeat++ {
				if err := writer.Send(fmt.Sprintf("m-%d-%d", index, repeat), "", "ingress.event", map[string]any{"eventId": fmt.Sprintf("evt-%d-%d", index, repeat)}); err != nil {
					t.Errorf("send: %v", err)
				}
			}
		}(index)
	}
	wait.Wait()
	reader := ipc.NewReader(&buffer, 4096)
	count := 0
	for {
		_, err := reader.Next()
		if err != nil {
			break
		}
		count++
	}
	if count != writers*8 {
		t.Fatalf("parsed %d frames, want %d", count, writers*8)
	}
}
