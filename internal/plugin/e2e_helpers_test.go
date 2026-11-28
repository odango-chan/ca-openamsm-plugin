package plugin_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"

	"github.com/odango-chan/ca-openamsm-plugin/internal/delivery"
	"github.com/odango-chan/ca-openamsm-plugin/internal/ipc"
	"github.com/odango-chan/ca-openamsm-plugin/internal/slot"
	"github.com/odango-chan/ca-openamsm-plugin/internal/staging"
)

var (
	buildOnce   sync.Once
	builtBinary string
	buildErr    error
)

func pluginBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		directory, err := os.MkdirTemp("", "openamsm-plugin-build")
		if err != nil {
			buildErr = err
			return
		}
		name := "ca-openamsm-plugin"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		target := filepath.Join(directory, name)
		command := exec.Command("go", "build", "-trimpath", "-o", target, "./cmd/openamsm-plugin")
		command.Dir = "../.."
		output, err := command.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("go build: %v: %s", err, output)
			return
		}
		builtBinary = target
	})
	if buildErr != nil {
		t.Fatalf("plugin build failed: %v", buildErr)
	}
	return builtBinary
}

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(payload)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

type testHost struct {
	t          *testing.T
	command    *exec.Cmd
	stdin      io.WriteCloser
	frames     chan ipc.Frame
	queue      []ipc.Frame
	stderr     *lockedBuffer
	inbound    string
	outbound   string
	address    string
	mu         sync.Mutex
	sequence   int
	exitSignal chan error
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func startTestHost(t *testing.T, extraArgs ...string) *testHost {
	t.Helper()
	executable := pluginBinary(t)
	port := freePort(t)
	inbound := t.TempDir()
	outbound := t.TempDir()

	arguments := append([]string{"serve"}, extraArgs...)
	command := exec.Command(executable, arguments...)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("stdin: %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}
	host := &testHost{
		t:          t,
		command:    command,
		stdin:      stdin,
		frames:     make(chan ipc.Frame, 64),
		stderr:     &lockedBuffer{},
		inbound:    inbound,
		outbound:   outbound,
		address:    fmt.Sprintf("127.0.0.1:%d", port),
		exitSignal: make(chan error, 1),
	}
	command.Stderr = host.stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start plugin: %v", err)
	}
	go host.readFrames(stdout)
	go func() {
		host.exitSignal <- command.Wait()
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	})

	initialize := map[string]any{
		"pluginId":        "urn:connectionagent:plugin:openamsm",
		"expectedVersion": "0.1.0",
		"ipcVersion":      ipc.APIVersion,
		"config": map[string]any{
			"peerId":                "test-peer",
			"externalRequestHeader": "X-CA-Request-Id",
			"sbi": map[string]any{
				"listenAddress":   host.address,
				"maxRequestBytes": 1 << 20,
			},
			"timeouts": map[string]any{
				"ackTimeoutMs":      3000,
				"deliveryTimeoutMs": 8000,
				"shutdownGraceMs":   2000,
			},
		},
		"artifactStaging": map[string]any{
			"inboundDirectory":  inbound,
			"outboundDirectory": outbound,
		},
	}
	host.sendFrame(host.newMessageID(), "", "host.initialize", initialize)
	ready := host.expect("plugin.ready", 15*time.Second)
	var readyPayload struct {
		PluginID      string   `json:"pluginId"`
		PluginVersion string   `json:"pluginVersion"`
		IPCVersion    string   `json:"ipcVersion"`
		Extensions    []string `json:"extensions"`
	}
	host.decode(ready.Payload, &readyPayload)
	if ready.CorrelationID == "" {
		host.t.Fatalf("plugin.ready must correlate with host.initialize")
	}
	if readyPayload.PluginID != "urn:connectionagent:plugin:openamsm" || readyPayload.PluginVersion != "0.1.0" || readyPayload.IPCVersion != ipc.APIVersion {
		host.t.Fatalf("plugin.ready identity = %+v", readyPayload)
	}
	if len(readyPayload.Extensions) != 2 {
		host.t.Fatalf("extensions = %v", readyPayload.Extensions)
	}
	host.waitForListener(5 * time.Second)
	return host
}

func (h *testHost) readFrames(reader io.Reader) {
	frames := ipc.NewReader(reader, 8<<20)
	for {
		frame, err := frames.Next()
		if err != nil {
			close(h.frames)
			return
		}
		h.frames <- frame
	}
}

func (h *testHost) waitForListener(timeout time.Duration) {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", h.address, 200*time.Millisecond)
		if err == nil {
			connection.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("SBI listener did not start at %s; stderr:\n%s", h.address, h.stderr.String())
}

func (h *testHost) newMessageID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sequence++
	return fmt.Sprintf("h-%d", h.sequence)
}

func (h *testHost) sendFrame(messageID, correlationID, messageType string, payload any) {
	h.t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		h.t.Fatalf("marshal %s payload: %v", messageType, err)
	}
	frame := ipc.Frame{
		APIVersion:    ipc.APIVersion,
		MessageID:     messageID,
		CorrelationID: correlationID,
		Type:          messageType,
		Payload:       encoded,
	}
	line, err := json.Marshal(frame)
	if err != nil {
		h.t.Fatalf("marshal frame: %v", err)
	}
	if _, err := h.stdin.Write(append(line, '\n')); err != nil {
		h.t.Fatalf("write frame: %v", err)
	}
}

func (h *testHost) expect(messageType string, timeout time.Duration) ipc.Frame {
	h.t.Helper()
	deadline := time.After(timeout)
	for {
		for index, frame := range h.queue {
			if frame.Type == messageType {
				h.queue = append(h.queue[:index], h.queue[index+1:]...)
				return frame
			}
		}
		select {
		case frame, ok := <-h.frames:
			if !ok {
				h.t.Fatalf("stdout closed while waiting for %s; stderr:\n%s", messageType, h.stderr.String())
			}
			if frame.Type == messageType {
				return frame
			}
			h.queue = append(h.queue, frame)
		case <-deadline:
			h.t.Fatalf("timeout waiting for %s; queued=%v; stderr:\n%s", messageType, frameTypes(h.queue), h.stderr.String())
		}
	}
}

func (h *testHost) expectNone(messageType string, wait time.Duration) {
	h.t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case frame, ok := <-h.frames:
			if !ok {
				return
			}
			if frame.Type == messageType {
				h.t.Fatalf("unexpected %s frame: %+v", messageType, frame)
			}
			h.queue = append(h.queue, frame)
		case <-deadline:
			return
		}
	}
}

func frameTypes(frames []ipc.Frame) []string {
	types := make([]string, 0, len(frames))
	for _, frame := range frames {
		types = append(types, frame.Type)
	}
	return types
}

func (h *testHost) decode(raw json.RawMessage, target any) {
	h.t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		h.t.Fatalf("decode payload: %v (raw=%s)", err, raw)
	}
}

func (h *testHost) waitExit(timeout time.Duration) (int, error) {
	h.t.Helper()
	select {
	case err := <-h.exitSignal:
		if err == nil {
			return 0, nil
		}
		var exitError *exec.ExitError
		if ok := asExitError(err, &exitError); ok {
			return exitError.ExitCode(), err
		}
		return -1, err
	case <-time.After(timeout):
		h.t.Fatalf("plugin did not exit; stderr:\n%s", h.stderr.String())
		return -1, nil
	}
}

func asExitError(err error, target **exec.ExitError) bool {
	exitError, ok := err.(*exec.ExitError)
	if ok {
		*target = exitError
	}
	return ok
}

func (h *testHost) shutdown() {
	h.t.Helper()
	messageID := h.newMessageID()
	h.sendFrame(messageID, "", "host.shutdown", map[string]any{"gracePeriodMs": 500})
	stopped := h.expect("plugin.stopped", 10*time.Second)
	var payload struct {
		InFlightCount int `json:"inFlightCount"`
	}
	h.decode(stopped.Payload, &payload)
	if stopped.CorrelationID != messageID {
		h.t.Fatalf("plugin.stopped correlation mismatch")
	}
	if payload.InFlightCount != 0 {
		h.t.Fatalf("inFlightCount = %d", payload.InFlightCount)
	}
	code, err := h.waitExit(10 * time.Second)
	if err != nil || code != 0 {
		h.t.Fatalf("shutdown exit = %v code=%d stderr=%s", err, code, h.stderr.String())
	}
}

// ingressFrame is the closed ingress.event payload for test assertions.
type ingressFrame struct {
	EventID           string `json:"eventId"`
	IngressID         string `json:"ingressId"`
	EventType         string `json:"eventType"`
	ExternalRequestID string `json:"externalRequestId"`
	ResponseChannelID string `json:"responseChannelId"`
	Source            struct {
		Protocol string `json:"protocol"`
		PeerID   string `json:"peerId"`
	} `json:"source"`
	Payload   json.RawMessage      `json:"payload"`
	Artifacts []staging.Descriptor `json:"artifacts"`
}

func decodeIngress(t *testing.T, frame ipc.Frame) ingressFrame {
	t.Helper()
	var ingress ingressFrame
	decoder := json.NewDecoder(bytes.NewReader(frame.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ingress); err != nil {
		t.Fatalf("decode ingress.event: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(frame.Payload, &raw); err != nil {
		t.Fatalf("decode ingress fields: %v", err)
	}
	if _, present := raw["correlationId"]; present {
		t.Fatalf("ingress.event payload must not carry correlationId")
	}
	return ingress
}

func (h *testHost) ack(eventFrame ipc.Frame, disposition string) {
	h.t.Helper()
	var ingress ingressFrame
	h.decode(eventFrame.Payload, &ingress)
	h.sendFrame(h.newMessageID(), eventFrame.MessageID, "ingress.ack", map[string]any{
		"eventId":     ingress.EventID,
		"disposition": disposition,
	})
}

func (h *testHost) dispatch(deliveryID, obligationID string, ingress ingressFrame, statusCode int, headers map[string]string, body []byte, artifacts []delivery.OutboundArtifact) string {
	h.t.Helper()
	messageID := h.newMessageID()
	payload := map[string]any{
		"deliveryId":           deliveryID,
		"responseObligationId": obligationID,
		"responseChannelId":    ingress.ResponseChannelID,
		"eventType":            ingress.EventType,
		"statusCode":           statusCode,
		"headers":              headers,
		"body":                 json.RawMessage(body),
		"artifacts":            artifacts,
	}
	h.sendFrame(messageID, "", "delivery.dispatch", payload)
	return messageID
}

func (h *testHost) expectDelivered(dispatchMessageID, deliveryID string) {
	h.t.Helper()
	result := h.expect("delivery.result", 10*time.Second)
	if result.CorrelationID != dispatchMessageID {
		h.t.Fatalf("delivery.result correlation = %q", result.CorrelationID)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(result.Payload, &fields); err != nil {
		h.t.Fatalf("decode result: %v", err)
	}
	allowed := map[string]bool{"deliveryId": true, "disposition": true, "statusCode": true, "error": true}
	for key := range fields {
		if !allowed[key] {
			h.t.Fatalf("delivery.result leaked field %q", key)
		}
	}
	var payload struct {
		DeliveryID  string `json:"deliveryId"`
		Disposition string `json:"disposition"`
		StatusCode  *int   `json:"statusCode"`
	}
	h.decode(result.Payload, &payload)
	if payload.DeliveryID != deliveryID {
		h.t.Fatalf("deliveryId = %q", payload.DeliveryID)
	}
	if payload.Disposition != delivery.DispositionDelivered {
		h.t.Fatalf("disposition = %q", payload.Disposition)
	}
	if payload.StatusCode == nil {
		h.t.Fatalf("delivered result must carry statusCode")
	}
}

func (h *testHost) expectFailed(dispatchMessageID, deliveryID, wantCode string) {
	h.t.Helper()
	result := h.expect("delivery.result", 10*time.Second)
	if result.CorrelationID != dispatchMessageID {
		h.t.Fatalf("delivery.result correlation = %q", result.CorrelationID)
	}
	var payload struct {
		DeliveryID  string              `json:"deliveryId"`
		Disposition string              `json:"disposition"`
		StatusCode  *int                `json:"statusCode"`
		Error       *delivery.SafeError `json:"error"`
	}
	h.decode(result.Payload, &payload)
	if payload.DeliveryID != deliveryID {
		h.t.Fatalf("deliveryId = %q", payload.DeliveryID)
	}
	if payload.Disposition != "failed" {
		h.t.Fatalf("disposition = %q", payload.Disposition)
	}
	if payload.Error == nil || payload.Error.Code != wantCode {
		h.t.Fatalf("error = %+v, want code %s", payload.Error, wantCode)
	}
}

func (h *testHost) writeOutbound(t *testing.T, slotName, contentID, relative string, payload []byte) delivery.OutboundArtifact {
	t.Helper()
	target := filepath.Join(h.outbound, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("outbound mkdir: %v", err)
	}
	if err := os.WriteFile(target, payload, 0o600); err != nil {
		t.Fatalf("outbound write: %v", err)
	}
	mediaType, _ := slot.MediaType(slotName)
	return delivery.OutboundArtifact{Slot: slotName, MediaType: mediaType, ContentID: contentID, StagingPath: relative, ByteLength: int64(len(payload))}
}

func (h *testHost) readStaged(t *testing.T, relative string) []byte {
	t.Helper()
	target, err := staging.ResolveWithin(h.inbound, relative)
	if err != nil {
		t.Fatalf("resolve staged %s: %v", relative, err)
	}
	payload, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read staged %s: %v", relative, err)
	}
	return payload
}

// http2Client returns an h2c prior-knowledge client. The Plugin listener is
// real HTTP/2 (no HTTP/1.1 fallback).
func http2Client() *http.Client {
	transport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, address string, _ *tls.Config) (net.Conn, error) {
			dialer := &net.Dialer{}
			return dialer.DialContext(ctx, network, address)
		},
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second}
}

type httpOutcome struct {
	status      int
	headers     http.Header
	body        []byte
	err         error
	contentType string
}

func (h *testHost) doAsync(request *http.Request) <-chan httpOutcome {
	h.t.Helper()
	return h.doAsyncWith(http2Client(), request)
}

func (h *testHost) doAsyncWith(client *http.Client, request *http.Request) <-chan httpOutcome {
	h.t.Helper()
	outcome := make(chan httpOutcome, 1)
	go func() {
		response, err := client.Do(request)
		if err != nil {
			outcome <- httpOutcome{err: err}
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		outcome <- httpOutcome{status: response.StatusCode, headers: response.Header, body: body, err: err, contentType: response.Header.Get("Content-Type")}
	}()
	return outcome
}

func (h *testHost) newRequest(method, path, contentType string, body []byte, headers map[string]string) *http.Request {
	h.t.Helper()
	request, err := http.NewRequest(method, "http://"+h.address+path, bytes.NewReader(body))
	if err != nil {
		h.t.Fatalf("new request: %v", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return request
}

type decodedMultipart struct {
	jsonPart  []byte
	parts     map[string][]byte
	mediaType map[string]string
}

func decodeMultipartResponse(t *testing.T, outcome httpOutcome) decodedMultipart {
	t.Helper()
	mediaType, parameters, err := mime.ParseMediaType(outcome.contentType)
	if err != nil || mediaType != "multipart/related" {
		t.Fatalf("content type = %q err=%v", outcome.contentType, err)
	}
	reader := multipart.NewReader(bytes.NewReader(outcome.body), parameters["boundary"])
	decoded := decodedMultipart{parts: map[string][]byte{}, mediaType: map[string]string{}}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		payload, _ := io.ReadAll(part)
		contentType := part.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "application/json") {
			decoded.jsonPart = payload
			continue
		}
		contentID := strings.Trim(strings.TrimSpace(part.Header.Get("Content-ID")), "<>")
		decoded.parts[contentID] = payload
		decoded.mediaType[contentID] = contentType
	}
	return decoded
}

func outcomeJSON(t *testing.T, outcome httpOutcome) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(outcome.body, &value); err != nil {
		t.Fatalf("decode diagnostic body %q: %v", outcome.body, err)
	}
	return value
}
