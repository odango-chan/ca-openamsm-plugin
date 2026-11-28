package plugin_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odango-chan/ca-openamsm-plugin/internal/delivery"
	"github.com/odango-chan/ca-openamsm-plugin/internal/ipc"
	"github.com/odango-chan/ca-openamsm-plugin/internal/slot"
	"github.com/odango-chan/ca-openamsm-plugin/internal/testsupport"
)

func (h *testHost) startCreate(t *testing.T, n1, n2 []byte, extraHeaders map[string]string, requestJSON []byte) (ipc.Frame, ingressFrame, <-chan httpOutcome) {
	t.Helper()
	parts := []testsupport.BinaryPart{{ContentID: "n1SmMsg", MediaType: testsupport.MediaNAS, Bytes: n1}}
	if n2 != nil {
		parts = append(parts, testsupport.BinaryPart{ContentID: "n2SmInfo", MediaType: testsupport.MediaNGAP, Bytes: n2})
	}
	contentType, body := testsupport.Multipart(requestJSON, parts...)
	request := h.newRequest("POST", "/sm-contexts", contentType, body, extraHeaders)
	outcome := h.doAsync(request)
	frame := h.expect("ingress.event", 15*time.Second)
	ingress := decodeIngress(t, frame)
	return frame, ingress, outcome
}

func TestAC00901LifecycleHandshakeAndShutdown(t *testing.T) {
	host := startTestHost(t)
	host.shutdown()
}

func TestAC00901RejectProtocolViolations(t *testing.T) {
	inbound := t.TempDir()
	outbound := t.TempDir()

	initializePayload := func(ipcVersion, expectedVersion string, withStaging bool) map[string]any {
		payload := map[string]any{
			"pluginId":        "urn:connectionagent:plugin:openamsm",
			"expectedVersion": expectedVersion,
			"ipcVersion":      ipcVersion,
			"config": map[string]any{
				"peerId": "raw-peer",
				"sbi":    map[string]any{"listenAddress": fmt.Sprintf("127.0.0.1:%d", freePort(t))},
			},
		}
		if withStaging {
			payload["artifactStaging"] = map[string]any{"inboundDirectory": inbound, "outboundDirectory": outbound}
		}
		return payload
	}

	t.Run("wrong-envelope-api-version", func(t *testing.T) {
		line := fmt.Sprintf(`{"apiVersion":"connectionagent.io/plugin-ipc/v2","messageId":"h-1","type":"host.initialize","payload":%s}`, mustJSON(t, initializePayload(ipc.APIVersion, "0.1.0", true)))
		code := runRawFrames(t, line)
		if code != 2 {
			t.Fatalf("exit code = %d", code)
		}
	})

	t.Run("initialize-identity-mismatch", func(t *testing.T) {
		code := runRawFrames(t, rawFrame(t, "h-1", "", "host.initialize", initializePayload(ipc.APIVersion, "9.9.9", true)))
		if code != 2 {
			t.Fatalf("exit code = %d", code)
		}
	})

	t.Run("missing-artifact-staging", func(t *testing.T) {
		code := runRawFrames(t, rawFrame(t, "h-1", "", "host.initialize", initializePayload(ipc.APIVersion, "0.1.0", false)))
		if code != 2 {
			t.Fatalf("exit code = %d", code)
		}
	})

	t.Run("unknown-message-type", func(t *testing.T) {
		code := runRawFrames(t,
			rawFrame(t, "h-1", "", "host.initialize", initializePayload(ipc.APIVersion, "0.1.0", true)),
			rawFrame(t, "h-2", "", "invoke.magic", map[string]any{}),
		)
		if code != 2 {
			t.Fatalf("exit code = %d", code)
		}
	})

	t.Run("duplicate-message-id", func(t *testing.T) {
		cancel := map[string]any{"targetOperationId": "evt-missing", "reason": "test"}
		code := runRawFrames(t,
			rawFrame(t, "h-1", "", "host.initialize", initializePayload(ipc.APIVersion, "0.1.0", true)),
			rawFrame(t, "h-2", "", "host.cancel", cancel),
			rawFrame(t, "h-2", "", "host.cancel", cancel),
		)
		if code != 2 {
			t.Fatalf("exit code = %d", code)
		}
	})

	t.Run("unsupported-argv", func(t *testing.T) {
		command := exec.Command(pluginBinary(t), "serve", "--not-a-real-flag")
		command.Stderr = &lockedBuffer{}
		err := command.Run()
		exitError, ok := err.(*exec.ExitError)
		if !ok || exitError.ExitCode() != 2 {
			t.Fatalf("err = %v", err)
		}
	})
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return encoded
}

func rawFrame(t *testing.T, messageID, correlationID, messageType string, payload any) string {
	t.Helper()
	frame := map[string]any{
		"apiVersion": ipc.APIVersion,
		"messageId":  messageID,
		"type":       messageType,
		"payload":    payload,
	}
	if correlationID != "" {
		frame["correlationId"] = correlationID
	}
	return string(mustJSON(t, frame))
}

func runRawFrames(t *testing.T, lines ...string) int {
	t.Helper()
	command := exec.Command(pluginBinary(t), "serve")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("stdin: %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}
	stderr := &lockedBuffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	go func() { _, _ = ioCopy(stdout) }()
	for _, line := range lines {
		if _, err := stdin.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			_ = stdin.Close()
			return 0
		}
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode()
		}
		t.Fatalf("wait: %v stderr=%s", err, stderr.String())
		return -1
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		t.Fatalf("plugin did not fail closed; stderr:\n%s", stderr.String())
		return -1
	}
}

func ioCopy(reader interface{ Read([]byte) (int, error) }) (int64, error) {
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		n, err := reader.Read(buffer)
		total += int64(n)
		if err != nil {
			return total, nil
		}
	}
}

func TestAC00902CreateEndToEndByteFidelity(t *testing.T) {
	host := startTestHost(t)
	frame, ingress, outcome := host.startCreate(t, testsupport.N1Modify, testsupport.N2SetupResponse, map[string]string{"X-CA-Request-Id": "client-1"}, testsupport.CreateJSON())

	if ingress.EventType != "nsmf.create-sm-context" {
		t.Fatalf("eventType = %q", ingress.EventType)
	}
	if ingress.IngressID != "urn:connectionagent:plugin:openamsm" {
		t.Fatalf("ingressId = %q", ingress.IngressID)
	}
	if ingress.ExternalRequestID != "client-1" {
		t.Fatalf("externalRequestId = %q", ingress.ExternalRequestID)
	}
	if ingress.Source.PeerID != "test-peer" || ingress.Source.Protocol == "" {
		t.Fatalf("source = %+v", ingress.Source)
	}
	if ingress.ResponseChannelID == "" {
		t.Fatalf("responseChannelId empty")
	}
	var payload struct {
		OperationID string          `json:"operationId"`
		Request     json.RawMessage `json:"request"`
	}
	host.decode(ingress.Payload, &payload)
	if payload.OperationID != "PostSmContexts" {
		t.Fatalf("operationId = %q", payload.OperationID)
	}
	if string(payload.Request) != string(testsupport.CreateJSON()) {
		t.Fatalf("request JSON rewritten")
	}
	if len(ingress.Artifacts) != 2 {
		t.Fatalf("artifacts = %d", len(ingress.Artifacts))
	}
	expected := []struct {
		slot      string
		mediaType string
		contentID string
		bytes     []byte
	}{
		{slot: slot.N1Request, mediaType: slot.MediaTypeNAS, contentID: "n1SmMsg", bytes: testsupport.N1Modify},
		{slot: slot.N2Request, mediaType: slot.MediaTypeNGAP, contentID: "n2SmInfo", bytes: testsupport.N2SetupResponse},
	}
	for index, want := range expected {
		artifact := ingress.Artifacts[index]
		if artifact.Slot != want.slot || artifact.MediaType != want.mediaType || artifact.ContentID != want.contentID {
			t.Fatalf("artifact[%d] = %+v", index, artifact)
		}
		if artifact.ByteLength != int64(len(want.bytes)) {
			t.Fatalf("artifact[%d].byteLength = %d", index, artifact.ByteLength)
		}
		if filepath.IsAbs(artifact.StagingPath) {
			t.Fatalf("stagingPath absolute: %q", artifact.StagingPath)
		}
		staged := host.readStaged(t, artifact.StagingPath)
		if !bytes.Equal(staged, want.bytes) {
			t.Fatalf("artifact[%d] staged bytes differ", index)
		}
	}
	if strings.Contains(string(frame.Payload), "artifactRef") {
		t.Fatalf("ingress.event leaked artifactRef")
	}

	host.ack(frame, "accepted")
	artifact := host.writeOutbound(t, slot.N1Response, "n1SmMsg", "response-1/n1.bin", testsupport.N1OwnerResponse)
	dispatchID := host.dispatch("del-1", "obl-1", ingress, 201, map[string]string{
		"Location": "https://smf.example.test/nsmf-pdusession/v1/sm-contexts/1",
	}, testsupport.Response201Body(), []delivery.OutboundArtifact{artifact})
	host.expectDelivered(dispatchID, "del-1")

	out := <-outcome
	if out.err != nil {
		t.Fatalf("HTTP outcome: %v stderr=%s", out.err, host.stderr.String())
	}
	if out.status != 201 {
		t.Fatalf("status = %d body=%s", out.status, out.body)
	}
	if out.headers.Get("Location") == "" {
		t.Fatalf("Location lost")
	}
	multipart := decodeMultipartResponse(t, out)
	if !bytes.Equal(multipart.jsonPart, testsupport.Response201Body()) {
		t.Fatalf("response JSON changed")
	}
	if !bytes.Equal(multipart.parts["n1SmMsg"], testsupport.N1OwnerResponse) {
		t.Fatalf("n1.response bytes changed")
	}
	if multipart.mediaType["n1SmMsg"] != slot.MediaTypeNAS {
		t.Fatalf("n1.response media type = %q", multipart.mediaType["n1SmMsg"])
	}
	host.shutdown()
}

func TestAC00902UpdateProductionClassifierBlocked(t *testing.T) {
	host := startTestHost(t)
	contentType, body := testsupport.Multipart(testsupport.UpdateJSONModify(),
		testsupport.BinaryPart{ContentID: "n1SmMsg", MediaType: testsupport.MediaNAS, Bytes: testsupport.N1Modify},
		testsupport.BinaryPart{ContentID: "n2SmInfo", MediaType: testsupport.MediaNGAP, Bytes: testsupport.N2ModResponse},
	)
	outcome := host.doAsync(host.newRequest("POST", "/sm-contexts/ref-prod/modify", contentType, body, nil))
	out := <-outcome
	if out.err != nil {
		t.Fatalf("outcome: %v", out.err)
	}
	if out.status != 503 {
		t.Fatalf("status = %d body=%s", out.status, out.body)
	}
	bodyFields := outcomeJSON(t, out)
	if bodyFields["code"] != "T0_CLASSIFIER_BLOCKED_EXTERNAL_INPUT" {
		t.Fatalf("diagnostic = %v", bodyFields)
	}
	host.expectNone("ingress.event", 300*time.Millisecond)
	entries, err := os.ReadDir(host.inbound)
	if err != nil {
		t.Fatalf("read inbound: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("blocked Update left staging residue: %v", entries)
	}
	host.shutdown()
}

func TestAC00902UpdateAndContinuationMappingWithTestDouble(t *testing.T) {
	host := startTestHost(t, "--test-only-classifier-double")

	updateContentType, updateBody := testsupport.Multipart(testsupport.UpdateJSONModify(),
		testsupport.BinaryPart{ContentID: "n1SmMsg", MediaType: testsupport.MediaNAS, Bytes: testsupport.N1Modify},
		testsupport.BinaryPart{ContentID: "n2SmInfo", MediaType: testsupport.MediaNGAP, Bytes: testsupport.N2ModResponse},
	)
	updateOutcome := host.doAsync(host.newRequest("POST", "/sm-contexts/ref-1/modify", updateContentType, updateBody, map[string]string{"X-CA-Request-Id": "modify-1"}))
	updateFrame := host.expect("ingress.event", 15*time.Second)
	updateIngress := decodeIngress(t, updateFrame)
	if updateIngress.EventType != "nsmf.update-sm-context" {
		t.Fatalf("eventType = %q", updateIngress.EventType)
	}
	var updatePayload struct {
		OperationID   string          `json:"operationId"`
		SmContextRef  string          `json:"smContextRef"`
		Request       json.RawMessage `json:"request"`
		Discriminator struct {
			N1MessageType string `json:"n1MessageType"`
			N2SmInfoType  string `json:"n2SmInfoType"`
		} `json:"discriminator"`
	}
	host.decode(updateIngress.Payload, &updatePayload)
	if updatePayload.SmContextRef != "ref-1" || updatePayload.Discriminator.N1MessageType != "0xC9" {
		t.Fatalf("payload = %+v", updatePayload)
	}
	if updatePayload.Discriminator.N2SmInfoType != "" {
		t.Fatalf("update discriminator must not carry n2SmInfoType")
	}
	if len(updateIngress.Artifacts) != 2 || updateIngress.Artifacts[0].Slot != slot.N1Request || updateIngress.Artifacts[1].Slot != slot.N2Request {
		t.Fatalf("artifacts = %+v", updateIngress.Artifacts)
	}
	host.ack(updateFrame, "accepted")
	updateDispatch := host.dispatch("del-update", "obl-update", updateIngress, 200, nil, []byte(`{}`), nil)
	host.expectDelivered(updateDispatch, "del-update")
	updateResult := <-updateOutcome
	if updateResult.status != 200 {
		t.Fatalf("update status = %d", updateResult.status)
	}

	continuationContentType, continuationBody := testsupport.Multipart(testsupport.UpdateN2ContinuationJSON("PDU_RES_SETUP_RSP"),
		testsupport.BinaryPart{ContentID: "n2SmInfo", MediaType: testsupport.MediaNGAP, Bytes: testsupport.N2SetupResponse},
	)
	continuationOutcome := host.doAsync(host.newRequest("POST", "/sm-contexts/ref-1/modify", continuationContentType, continuationBody, nil))
	continuationFrame := host.expect("ingress.event", 15*time.Second)
	continuationIngress := decodeIngress(t, continuationFrame)
	if continuationIngress.EventType != "nsmf.update-sm-context.continuation" {
		t.Fatalf("eventType = %q", continuationIngress.EventType)
	}
	var continuationPayload struct {
		OperationID   string          `json:"operationId"`
		SmContextRef  string          `json:"smContextRef"`
		Request       json.RawMessage `json:"request"`
		Discriminator struct {
			N2SmInfoType string `json:"n2SmInfoType"`
		} `json:"discriminator"`
	}
	host.decode(continuationIngress.Payload, &continuationPayload)
	if continuationPayload.Discriminator.N2SmInfoType != "PDU_RES_SETUP_RSP" {
		t.Fatalf("discriminator = %+v", continuationPayload)
	}
	if len(continuationIngress.Artifacts) != 1 || continuationIngress.Artifacts[0].Slot != slot.N2Confirmation {
		t.Fatalf("artifacts = %+v", continuationIngress.Artifacts)
	}
	host.ack(continuationFrame, "accepted")
	continuationDispatch := host.dispatch("del-cont", "obl-cont", continuationIngress, 204, nil, nil, nil)
	host.expectDelivered(continuationDispatch, "del-cont")
	continuationResult := <-continuationOutcome
	if continuationResult.status != 204 || len(continuationResult.body) != 0 {
		t.Fatalf("continuation status=%d body=%q", continuationResult.status, continuationResult.body)
	}
	host.shutdown()
}

func TestAC00902CreateWithoutN2IsAccepted(t *testing.T) {
	// n2.request is declared optional; a Create that actually carries no N2 part
	// must still normalize to a single n1.request artifact.
	host := startTestHost(t)
	frame, ingress, outcome := host.startCreate(t, testsupport.N1Modify, nil, nil, testsupport.CreateJSONN1Only())
	if ingress.EventType != "nsmf.create-sm-context" {
		t.Fatalf("eventType = %q", ingress.EventType)
	}
	if len(ingress.Artifacts) != 1 || ingress.Artifacts[0].Slot != slot.N1Request {
		t.Fatalf("artifacts = %+v", ingress.Artifacts)
	}
	host.ack(frame, "accepted")
	dispatchID := host.dispatch("del-n2-free", "obl-n2-free", ingress, 201, nil, testsupport.Response201Body(), nil)
	host.expectDelivered(dispatchID, "del-n2-free")
	out := <-outcome
	if out.status != 201 {
		t.Fatalf("status = %d", out.status)
	}
	host.shutdown()
}

func TestAC00905UnknownChannelAndMissingObligation(t *testing.T) {
	host := startTestHost(t)
	unknownID := host.newMessageID()
	host.sendFrame(unknownID, "", "delivery.dispatch", map[string]any{
		"deliveryId":           "del-missing",
		"responseObligationId": "obl-missing",
		"responseChannelId":    "ch-missing",
		"eventType":            "nsmf.create-sm-context",
		"statusCode":           200,
		"headers":              map[string]string{},
		"body":                 json.RawMessage(`{}`),
		"artifacts":            []any{},
	})
	host.expectFailed(unknownID, "del-missing", "UNKNOWN_RESPONSE_CHANNEL")

	missingID := host.newMessageID()
	host.sendFrame(missingID, "", "delivery.dispatch", map[string]any{
		"deliveryId":           "del-no-obligation",
		"responseObligationId": "",
		"responseChannelId":    "ch-any",
		"eventType":            "nsmf.create-sm-context",
		"statusCode":           200,
		"headers":              map[string]string{},
		"body":                 json.RawMessage(`{}`),
		"artifacts":            []any{},
	})
	host.expectFailed(missingID, "del-no-obligation", "RESPONSE_OBLIGATION_MISSING")
	host.shutdown()
}

func TestAC00905WrongObligationSecondDispatchFails(t *testing.T) {
	host := startTestHost(t)
	frame, ingress, outcome := host.startCreate(t, testsupport.N1Modify, testsupport.N2SetupResponse, nil, testsupport.CreateJSON())
	host.ack(frame, "accepted")
	firstDispatch := host.dispatch("del-1", "obl-1", ingress, 201, nil, testsupport.Response201Body(), nil)
	host.expectDelivered(firstDispatch, "del-1")
	first := <-outcome
	if first.status != 201 {
		t.Fatalf("first status = %d", first.status)
	}

	secondDispatch := host.dispatch("del-2", "obl-2", ingress, 201, nil, testsupport.Response201Body(), nil)
	host.expectFailed(secondDispatch, "del-2", "UNKNOWN_RESPONSE_CHANNEL")
	host.expectNone("delivery.result", 200*time.Millisecond)
	host.shutdown()
}

func TestAC00905AckLeakFailsClosed(t *testing.T) {
	for _, forbidden := range []string{"responseObligationId", "artifactRef", "runId", "deliveryId", "skillId"} {
		t.Run(forbidden, func(t *testing.T) {
			host := startTestHost(t)
			frame, ingress, outcome := host.startCreate(t, testsupport.N1Modify, testsupport.N2SetupResponse, nil, testsupport.CreateJSON())
			host.sendFrame(host.newMessageID(), frame.MessageID, "ingress.ack", map[string]any{
				"eventId":     ingress.EventID,
				"disposition": "accepted",
				forbidden:     "leak",
			})
			code, _ := host.waitExit(10 * time.Second)
			if code != 2 {
				t.Fatalf("exit code = %d stderr=%s", code, host.stderr.String())
			}
			host.expectNone("delivery.result", 200*time.Millisecond)
			select {
			case out := <-outcome:
				if out.err == nil && out.status < 400 {
					t.Fatalf("leaked ack must not produce a successful response: %d", out.status)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("stream did not terminate")
			}
		})
	}
}

func TestAC00905CancelLiveIngress(t *testing.T) {
	host := startTestHost(t)
	frame, ingress, outcome := host.startCreate(t, testsupport.N1Modify, testsupport.N2SetupResponse, nil, testsupport.CreateJSON())
	cancelID := host.newMessageID()
	host.sendFrame(cancelID, "", "host.cancel", map[string]any{"targetOperationId": ingress.EventID, "reason": "operator_cancel"})
	cancelled := host.expect("plugin.cancelled", 5*time.Second)
	if cancelled.CorrelationID != cancelID {
		t.Fatalf("cancelled correlation = %q", cancelled.CorrelationID)
	}
	var cancelledPayload struct {
		TargetOperationID string `json:"targetOperationId"`
		Disposition       string `json:"disposition"`
	}
	host.decode(cancelled.Payload, &cancelledPayload)
	if cancelledPayload.TargetOperationID != ingress.EventID || cancelledPayload.Disposition != "cancelled" {
		t.Fatalf("cancelled = %+v", cancelledPayload)
	}
	out := <-outcome
	if out.status != 503 {
		t.Fatalf("cancelled ingress status = %d body=%s", out.status, out.body)
	}
	if outcomeJSON(t, out)["code"] != "INGRESS_CANCELLED" {
		t.Fatalf("diagnostic = %s", out.body)
	}

	lateID := host.newMessageID()
	host.sendFrame(lateID, "", "host.cancel", map[string]any{"targetOperationId": "evt-unknown", "reason": "retry"})
	late := host.expect("plugin.cancelled", 5*time.Second)
	var latePayload struct {
		TargetOperationID string `json:"targetOperationId"`
		TargetMessageID   string `json:"targetMessageId"`
		Disposition       string `json:"disposition"`
	}
	host.decode(late.Payload, &latePayload)
	if latePayload.Disposition != "too_late" {
		t.Fatalf("late cancel disposition = %q", latePayload.Disposition)
	}
	_ = frame
	host.shutdown()
}

func TestAC00903PathTraversalOutboundDispatchFails(t *testing.T) {
	host := startTestHost(t)
	frame, ingress, outcome := host.startCreate(t, testsupport.N1Modify, testsupport.N2SetupResponse, nil, testsupport.CreateJSON())
	host.ack(frame, "accepted")
	artifact := delivery.OutboundArtifact{
		Slot:        slot.N1Response,
		MediaType:   slot.MediaTypeNAS,
		ContentID:   "n1SmMsg",
		StagingPath: "../escape.bin",
		ByteLength:  3,
	}
	dispatchID := host.dispatch("del-traversal", "obl-traversal", ingress, 201, nil, testsupport.Response201Body(), []delivery.OutboundArtifact{artifact})
	host.expectFailed(dispatchID, "del-traversal", "DELIVERY_ARTIFACT_STAGING_PATH_ESCAPE")
	out := <-outcome
	if out.status != 502 {
		t.Fatalf("status = %d body=%s", out.status, out.body)
	}
	if outcomeJSON(t, out)["code"] != "DELIVERY_ARTIFACT_STAGING_PATH_ESCAPE" {
		t.Fatalf("diagnostic = %s", out.body)
	}
	host.shutdown()
}

func TestAC00906ConcurrentStreamsIsolation(t *testing.T) {
	host := startTestHost(t)
	const streams = 8
	client := http2Client()

	type job struct {
		index      int
		externalID string
		n1         []byte
		frame      ipc.Frame
		ingress    ingressFrame
		outcome    <-chan httpOutcome
		n1Response []byte
	}

	jobs := make([]*job, streams)
	typeRequest := make(map[string]*job)
	for index := 0; index < streams; index++ {
		externalID := fmt.Sprintf("client-%d", index)
		requestJSON := []byte(fmt.Sprintf(`{"n1SmMsg":{"contentId":"n1SmMsg"},"index":%d,"marker":"%s"}`, index, externalID))
		contentType, body := testsupport.Multipart(requestJSON,
			testsupport.BinaryPart{ContentID: "n1SmMsg", MediaType: testsupport.MediaNAS, Bytes: append(testsupport.N1Modify, byte(index))},
		)
		outcome := host.doAsyncWith(client, host.newRequest("POST", "/sm-contexts", contentType, body, map[string]string{"X-CA-Request-Id": externalID}))
		jobs[index] = &job{index: index, externalID: externalID, outcome: outcome, n1: append(testsupport.N1Modify, byte(index))}
	}

	for index := 0; index < streams; index++ {
		frame := host.expect("ingress.event", 20*time.Second)
		ingress := decodeIngress(t, frame)
		target, ok := typeRequest[ingress.ExternalRequestID]
		if ok {
			t.Fatalf("duplicate externalRequestId %q", ingress.ExternalRequestID)
		}
		for _, candidate := range jobs {
			if candidate.externalID == ingress.ExternalRequestID {
				target = candidate
				break
			}
		}
		if target == nil {
			t.Fatalf("unexpected externalRequestId %q", ingress.ExternalRequestID)
		}
		target.frame = frame
		target.ingress = ingress
		typeRequest[ingress.ExternalRequestID] = target
	}

	dispatchExternalIDs := make(map[string]string)
	for _, candidate := range jobs {
		if candidate.ingress.EventID == "" {
			t.Fatalf("missing ingress for %s", candidate.externalID)
		}
		host.ack(candidate.frame, "accepted")
		n1Response := append(testsupport.N1OwnerResponse, byte(candidate.index))
		candidate.n1Response = n1Response
		artifact := host.writeOutbound(t, slot.N1Response, "n1SmMsg", fmt.Sprintf("response-%d/n1.bin", candidate.index), n1Response)
		body := []byte(fmt.Sprintf(`{"request":"%s"}`, candidate.externalID))
		dispatchID := host.dispatch(fmt.Sprintf("del-%d", candidate.index), fmt.Sprintf("obl-%d", candidate.index), candidate.ingress, 200, nil, body, []delivery.OutboundArtifact{artifact})
		dispatchExternalIDs[dispatchID] = candidate.externalID
	}

	for index := 0; index < streams; index++ {
		result := host.expect("delivery.result", 20*time.Second)
		externalID, ok := dispatchExternalIDs[result.CorrelationID]
		if !ok {
			t.Fatalf("unexpected delivery.result correlation %q", result.CorrelationID)
		}
		var payload struct {
			DeliveryID  string `json:"deliveryId"`
			Disposition string `json:"disposition"`
			StatusCode  *int   `json:"statusCode"`
		}
		host.decode(result.Payload, &payload)
		if payload.Disposition != "delivered" {
			t.Fatalf("stream %s disposition = %q", externalID, payload.Disposition)
		}
	}

	for _, candidate := range jobs {
		out := <-candidate.outcome
		if out.err != nil {
			t.Fatalf("%s outcome: %v", candidate.externalID, out.err)
		}
		if out.status != 200 {
			t.Fatalf("%s status = %d", candidate.externalID, out.status)
		}
		decoded := decodeMultipartResponse(t, out)
		var body struct {
			Request string `json:"request"`
		}
		if err := json.Unmarshal(decoded.jsonPart, &body); err != nil {
			t.Fatalf("%s response body: %v", candidate.externalID, err)
		}
		if body.Request != candidate.externalID {
			t.Fatalf("%s received response for %q", candidate.externalID, body.Request)
		}
		if !bytes.Equal(decoded.parts["n1SmMsg"], candidate.n1Response) {
			t.Fatalf("%s received cross-stream N1 bytes", candidate.externalID)
		}
	}
	host.shutdown()
}

func TestAC00906DuplicateRetryIndependentChannels(t *testing.T) {
	host := startTestHost(t)
	const externalID = "retry-client"
	requestJSON := testsupport.CreateJSON()
	headers := map[string]string{"X-CA-Request-Id": externalID}

	firstFrame, firstIngress, firstOutcome := host.startCreate(t, testsupport.N1Modify, testsupport.N2SetupResponse, headers, requestJSON)
	secondFrame, secondIngress, secondOutcome := host.startCreate(t, testsupport.N1Modify, testsupport.N2SetupResponse, headers, requestJSON)

	if firstIngress.EventID == secondIngress.EventID {
		t.Fatalf("duplicate retry reused eventId")
	}
	if firstIngress.ResponseChannelID == secondIngress.ResponseChannelID {
		t.Fatalf("duplicate retry reused responseChannelId")
	}
	host.ack(firstFrame, "accepted")
	host.ack(secondFrame, "duplicate_attached")

	firstDispatch := host.dispatch("del-first", "obl-first", firstIngress, 200, nil, []byte(`{"stream":"first"}`), nil)
	secondDispatch := host.dispatch("del-second", "obl-second", secondIngress, 200, nil, []byte(`{"stream":"second"}`), nil)
	host.expectDelivered(firstDispatch, "del-first")
	host.expectDelivered(secondDispatch, "del-second")

	firstOut := <-firstOutcome
	secondOut := <-secondOutcome
	if string(firstOut.body) != `{"stream":"first"}` || string(secondOut.body) != `{"stream":"second"}` {
		t.Fatalf("retry streams mixed: %s / %s", firstOut.body, secondOut.body)
	}
	host.shutdown()
}

func TestAC00901ShutdownWithLiveIngress(t *testing.T) {
	host := startTestHost(t)
	_, ingress, outcome := host.startCreate(t, testsupport.N1Modify, testsupport.N2SetupResponse, nil, testsupport.CreateJSON())
	_ = ingress
	messageID := host.newMessageID()
	host.sendFrame(messageID, "", "host.shutdown", map[string]any{"gracePeriodMs": 1000})
	stopped := host.expect("plugin.stopped", 10*time.Second)
	var payload struct {
		InFlightCount int `json:"inFlightCount"`
	}
	host.decode(stopped.Payload, &payload)
	if payload.InFlightCount < 1 {
		t.Fatalf("inFlightCount = %d", payload.InFlightCount)
	}
	code, err := host.waitExit(10 * time.Second)
	if err != nil || code != 0 {
		t.Fatalf("shutdown exit=%v code=%d", err, code)
	}
	select {
	case out := <-outcome:
		if out.err == nil && out.status < 500 {
			t.Fatalf("in-flight ingress must not succeed after shutdown: %d", out.status)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("in-flight stream did not terminate")
	}
}
