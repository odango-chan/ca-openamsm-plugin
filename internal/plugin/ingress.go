package plugin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/odango-chan/ca-openamsm-plugin/internal/delivery"
	"github.com/odango-chan/ca-openamsm-plugin/internal/event"
	"github.com/odango-chan/ca-openamsm-plugin/internal/sbi"
	"github.com/odango-chan/ca-openamsm-plugin/internal/staging"
)

type sourceIdentity struct {
	Protocol string `json:"protocol"`
	PeerID   string `json:"peerId"`
}

type ingressEventPayload struct {
	EventID           string               `json:"eventId"`
	IngressID         string               `json:"ingressId"`
	EventType         string               `json:"eventType"`
	ExternalRequestID string               `json:"externalRequestId,omitempty"`
	ResponseChannelID string               `json:"responseChannelId"`
	Source            sourceIdentity       `json:"source"`
	Payload           json.RawMessage      `json:"payload"`
	Artifacts         []staging.Descriptor `json:"artifacts"`
}

type safeDiagnostic struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (p *Plugin) handleIngress(w http.ResponseWriter, r *http.Request) {
	if p.shuttingDown.Load() {
		writeDiagnostic(w, http.StatusServiceUnavailable, "PLUGIN_SHUTTING_DOWN", "plugin is shutting down")
		return
	}
	p.inFlight.Add(1)
	defer p.inFlight.Done()

	r.Body = http.MaxBytesReader(w, r.Body, p.config.SBI.MaxRequestBytes)
	parsed, err := sbi.ParseRequest(r, p.config.SBI.MaxRequestBytes)
	if err != nil {
		code, status := sbiDiagnostic(err)
		writeDiagnostic(w, status, code, "SBI request rejected")
		return
	}
	normalized, err := event.Normalize(parsed, p.classifier)
	if err != nil {
		code, status := eventDiagnostic(err)
		writeDiagnostic(w, status, code, "normalized Event not produced")
		return
	}

	eventID := p.nextMessageID("evt")
	descriptors := make([]staging.Descriptor, 0, len(normalized.Parts))
	for _, part := range normalized.Parts {
		descriptor, err := p.store.WriteInbound(eventID, part.Slot, part.ContentID, part.MediaType, part.Bytes)
		if err != nil {
			_ = p.store.RemoveEvent(eventID)
			code, status := stagingDiagnostic(err)
			writeDiagnostic(w, status, code, "raw N1/N2 staging rejected")
			return
		}
		descriptors = append(descriptors, descriptor)
	}

	waiter, err := p.registry.Register(eventID, normalized.Type)
	if err != nil {
		_ = p.store.RemoveEvent(eventID)
		writeDiagnostic(w, http.StatusInternalServerError, "PLUGIN_INTERNAL", "waiter registration failed")
		return
	}
	ingressMessageID := p.nextMessageID("m")
	if err := p.registry.BindIngressMessage(waiter.ChannelID, ingressMessageID); err != nil {
		p.registry.Remove(waiter.ChannelID)
		_ = p.store.RemoveEvent(eventID)
		writeDiagnostic(w, http.StatusInternalServerError, "PLUGIN_INTERNAL", "waiter binding failed")
		return
	}

	externalRequestID := ""
	if header := p.config.ExternalRequestHeader; header != "" {
		if value := strings.TrimSpace(r.Header.Get(header)); value != "" && len(value) <= 256 {
			externalRequestID = value
		}
	}
	payload := ingressEventPayload{
		EventID:           eventID,
		IngressID:         p.manifest.PluginID,
		EventType:         normalized.Type,
		ExternalRequestID: externalRequestID,
		ResponseChannelID: waiter.ChannelID,
		Source:            sourceIdentity{Protocol: "nsmf-pdusession", PeerID: p.config.PeerID},
		Payload:           normalized.Payload,
		Artifacts:         descriptors,
	}
	if err := p.writer.Send(ingressMessageID, "", "ingress.event", payload); err != nil {
		p.registry.Remove(waiter.ChannelID)
		_ = p.store.RemoveEvent(eventID)
		writeDiagnostic(w, http.StatusInternalServerError, "PLUGIN_IPC_UNAVAILABLE", "event publication failed")
		return
	}

	ackTimer := time.NewTimer(time.Duration(p.config.ackTimeout()) * time.Millisecond)
	defer ackTimer.Stop()
	select {
	case ack := <-waiter.AckCh:
		if ack.Disposition == delivery.AckRejected || ack.Disposition == delivery.AckConflict {
			p.registry.Remove(waiter.ChannelID)
			_ = p.store.RemoveEvent(eventID)
			status := http.StatusBadRequest
			if ack.Disposition == delivery.AckConflict {
				status = http.StatusConflict
			}
			writeDiagnostic(w, status, "INGRESS_"+strings.ToUpper(ack.Disposition), "Host did not accept the Event")
			return
		}
	case <-waiter.CancelCh:
		p.registry.Remove(waiter.ChannelID)
		_ = p.store.RemoveEvent(eventID)
		writeDiagnostic(w, http.StatusServiceUnavailable, "INGRESS_CANCELLED", "ingress was cancelled by the Host")
		return
	case <-ackTimer.C:
		p.registry.Remove(waiter.ChannelID)
		_ = p.store.RemoveEvent(eventID)
		writeDiagnostic(w, http.StatusGatewayTimeout, "INGRESS_ACK_TIMEOUT", "Host did not acknowledge the Event")
		return
	case <-r.Context().Done():
		p.registry.Remove(waiter.ChannelID)
		_ = p.store.RemoveEvent(eventID)
		return
	}

	deliveryTimer := time.NewTimer(time.Duration(p.config.deliveryTimeout()) * time.Millisecond)
	defer deliveryTimer.Stop()
	select {
	case dispatch := <-waiter.DispatchCh:
		result := p.writeDeliveredResponse(w, dispatch)
		waiter.ResultCh <- result
	case <-waiter.CancelCh:
		writeDiagnostic(w, http.StatusServiceUnavailable, "INGRESS_CANCELLED", "ingress was cancelled by the Host")
	case <-deliveryTimer.C:
		writeDiagnostic(w, http.StatusGatewayTimeout, "DELIVERY_TIMEOUT", "Host did not dispatch a response")
	case <-r.Context().Done():
	}
	p.registry.Remove(waiter.ChannelID)
}

// writeDeliveredResponse writes the Host-supplied SBI response to the exact live
// HTTP/2 stream. Only Host-supplied status, headers, body and staged artifacts
// are used; the Plugin invents no Location URI and no business payload.
func (p *Plugin) writeDeliveredResponse(w http.ResponseWriter, dispatch delivery.Dispatch) delivery.Result {
	parts := make([]sbi.OutboundPart, 0, len(dispatch.Artifacts))
	for _, artifact := range dispatch.Artifacts {
		payload, err := p.store.ReadOutbound(staging.Descriptor{
			Slot:        artifact.Slot,
			MediaType:   artifact.MediaType,
			ContentID:   artifact.ContentID,
			StagingPath: artifact.StagingPath,
			ByteLength:  artifact.ByteLength,
		})
		if err != nil {
			code := "DELIVERY_ARTIFACT_INVALID"
			var stagingErr *staging.Error
			if ok := asStagingError(err, &stagingErr); ok {
				code = "DELIVERY_ARTIFACT_" + stagingErr.Code
			}
			writeDiagnostic(w, http.StatusBadGateway, code, "outbound Artifact rejected")
			return delivery.Failed(code, "outbound Artifact rejected")
		}
		parts = append(parts, sbi.OutboundPart{
			Slot:      artifact.Slot,
			MediaType: artifact.MediaType,
			ContentID: artifact.ContentID,
			Bytes:     payload,
		})
	}
	if int64(len(dispatch.Body)) > p.config.Limits.MaxResponseBytes {
		writeDiagnostic(w, http.StatusBadGateway, "DELIVERY_BODY_TOO_LARGE", "response body exceeds the limit")
		return delivery.Failed("DELIVERY_BODY_TOO_LARGE", "response body exceeds the limit")
	}
	header, body, err := sbi.BuildResponse(dispatch.StatusCode, dispatch.Headers, dispatch.Body, parts)
	if err != nil {
		writeDiagnostic(w, http.StatusBadGateway, "DELIVERY_RESPONSE_INVALID", "Host response rejected")
		return delivery.Failed("DELIVERY_RESPONSE_INVALID", "Host response rejected")
	}
	for name, values := range header {
		for _, value := range values {
			w.Header().Set(name, value)
		}
	}
	w.WriteHeader(dispatch.StatusCode)
	if len(body) > 0 {
		if _, err := w.Write(body); err != nil {
			return delivery.Failed("DELIVERY_WRITE_FAILED", "live response stream closed")
		}
	}
	return delivery.Delivered(dispatch.StatusCode)
}

func writeDiagnostic(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	payload, _ := json.Marshal(safeDiagnostic{Code: code, Message: message, Retryable: false})
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func sbiDiagnostic(err error) (string, int) {
	var value *sbi.Error
	if !asSBIError(err, &value) {
		return "SBI_REQUEST_INVALID", http.StatusBadRequest
	}
	switch value.Code {
	case sbi.CodeMethodNotAllowed:
		return value.Code, http.StatusMethodNotAllowed
	case sbi.CodePathUnsupported, sbi.CodeSmContextRefInvalid:
		return value.Code, http.StatusNotFound
	case sbi.CodeBodyTooLarge:
		return value.Code, http.StatusRequestEntityTooLarge
	default:
		return value.Code, http.StatusBadRequest
	}
}

func eventDiagnostic(err error) (string, int) {
	var value *event.Error
	if !asEventError(err, &value) {
		return "EVENT_INVALID", http.StatusBadRequest
	}
	switch value.Code {
	case event.CodeT0Blocked:
		return "T0_CLASSIFIER_BLOCKED_EXTERNAL_INPUT", http.StatusServiceUnavailable
	case event.CodeN1Malformed, event.CodeN1Unsupported,
		event.CodeConflictingDiscriminator, event.CodeN2TypeUnknown,
		event.CodeN2TypeMissing, event.CodeN2TypeWithoutPart,
		event.CodeN2Ext1TypeWithoutPart, event.CodeN2Ext1ContinuationUnspec,
		event.CodeMissingRequiredSlot, event.CodeRequestInvalid:
		return value.Code, http.StatusBadRequest
	default:
		return value.Code, http.StatusBadRequest
	}
}

func stagingDiagnostic(err error) (string, int) {
	var value *staging.Error
	if !asStagingError(err, &value) {
		return "STAGING_WRITE_FAILED", http.StatusInternalServerError
	}
	return value.Code, http.StatusInternalServerError
}
