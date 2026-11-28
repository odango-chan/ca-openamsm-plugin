package plugin

import (
	"encoding/json"
	"time"

	"github.com/odango-chan/ca-openamsm-plugin/internal/delivery"
	"github.com/odango-chan/ca-openamsm-plugin/internal/ipc"
)

// handleAck enforces the closed ingress.ack allowlist. Any Host identity field
// (runId, skillId, artifactRef, responseObligationId, deliveryId) fails strict
// decoding and therefore terminates the process as a protocol violation.
func (p *Plugin) handleAck(frame ipc.Frame) error {
	ack, err := ipc.DecodePayload[delivery.Ack](frame.Payload)
	if err != nil {
		return err
	}
	if frame.CorrelationID == "" {
		return p.violation("ACK_CORRELATION_MISSING", frame.Type)
	}
	if ack.EventID == "" {
		return p.violation("ACK_EVENT_ID_MISSING", frame.Type)
	}
	if !delivery.ValidAckDisposition(ack.Disposition) {
		return p.violation("ACK_DISPOSITION_INVALID", ack.Disposition)
	}
	waiter := p.registry.ByIngressMessage(frame.CorrelationID)
	if waiter == nil {
		return p.violation("ACK_UNKNOWN_CORRELATION", frame.CorrelationID)
	}
	if waiter.EventID != ack.EventID {
		return p.violation("ACK_EVENT_MISMATCH", ack.EventID)
	}
	select {
	case waiter.AckCh <- ack:
		return nil
	default:
		return p.violation("ACK_DUPLICATE", ack.EventID)
	}
}

type dispatchFailure struct {
	code    string
	message string
}

func validateDispatch(dispatch delivery.Dispatch) *dispatchFailure {
	if dispatch.DeliveryID == "" {
		return &dispatchFailure{code: "DELIVERY_ID_MISSING", message: "deliveryId is required"}
	}
	if dispatch.ResponseObligationID == "" {
		return &dispatchFailure{code: "RESPONSE_OBLIGATION_MISSING", message: "responseObligationId is required"}
	}
	if dispatch.ResponseChannelID == "" {
		return &dispatchFailure{code: "RESPONSE_CHANNEL_MISSING", message: "responseChannelId is required"}
	}
	if dispatch.EventType == "" {
		return &dispatchFailure{code: "EVENT_TYPE_MISSING", message: "eventType is required"}
	}
	if dispatch.StatusCode < 100 || dispatch.StatusCode > 599 {
		return &dispatchFailure{code: "STATUS_CODE_INVALID", message: "statusCode must be an HTTP status"}
	}
	if body := json.Valid(dispatch.Body); !body && len(dispatch.Body) > 0 {
		return &dispatchFailure{code: "BODY_INVALID", message: "body must be valid JSON"}
	}
	for _, artifact := range dispatch.Artifacts {
		if artifact.Slot == "" || artifact.MediaType == "" || artifact.StagingPath == "" || artifact.ByteLength < 0 {
			return &dispatchFailure{code: "ARTIFACT_DESCRIPTOR_INVALID", message: "incomplete outbound artifact descriptor"}
		}
	}
	return nil
}

// handleDispatch routes one Delivery to the live response waiter. The Host-owned
// responseObligationId is used only transiently inside the dispatch value; it is
// never stored on the waiter and never appears in delivery.result.
func (p *Plugin) handleDispatch(frame ipc.Frame) error {
	dispatch, err := ipc.DecodePayload[delivery.Dispatch](frame.Payload)
	if err != nil {
		return err
	}
	if failure := validateDispatch(dispatch); failure != nil {
		p.sendResult(frame.MessageID, dispatch.DeliveryID, delivery.Failed(failure.code, failure.message))
		return nil
	}
	waiter, reason := p.registry.Claim(dispatch.ResponseChannelID, dispatch.EventType)
	if waiter == nil {
		p.sendResult(frame.MessageID, dispatch.DeliveryID, delivery.Failed(reason, "no live response channel for this dispatch"))
		return nil
	}
	select {
	case waiter.DispatchCh <- dispatch:
	default:
		p.sendResult(frame.MessageID, dispatch.DeliveryID, delivery.Failed("DISPATCH_DUPLICATE", "dispatch already queued for this channel"))
		return nil
	}
	select {
	case result := <-waiter.ResultCh:
		p.sendResult(frame.MessageID, dispatch.DeliveryID, result)
	case <-time.After(time.Duration(p.config.deliveryTimeout()) * time.Millisecond):
		p.sendResult(frame.MessageID, dispatch.DeliveryID, delivery.Failed("DELIVERY_RESULT_TIMEOUT", "live stream did not complete the response"))
	}
	return nil
}

func (p *Plugin) sendResult(correlationID, deliveryID string, result delivery.Result) {
	messageID := p.nextMessageID("d")
	if err := p.writer.Send(messageID, correlationID, "delivery.result", result.Payload(deliveryID)); err != nil {
		p.logger.Printf("DELIVERY_RESULT_NOT_SENT code=%s", err)
	}
}

type cancelPayload struct {
	TargetMessageID   string `json:"targetMessageId"`
	TargetOperationID string `json:"targetOperationId"`
	Reason            string `json:"reason"`
}

type cancelledPayload struct {
	TargetMessageID   string `json:"targetMessageId,omitempty"`
	TargetOperationID string `json:"targetOperationId,omitempty"`
	Disposition       string `json:"disposition"`
}

func (p *Plugin) handleCancel(frame ipc.Frame) error {
	payload, err := ipc.DecodePayload[cancelPayload](frame.Payload)
	if err != nil {
		return err
	}
	hasMessage := payload.TargetMessageID != ""
	hasOperation := payload.TargetOperationID != ""
	if hasMessage == hasOperation {
		return p.violation("CANCEL_TARGET_INVALID", frame.Type)
	}
	if hasOperation {
		if waiter := p.registry.CancelEvent(payload.TargetOperationID); waiter != nil {
			waiter.Cancel("cancelled")
			return p.sendCancelled(frame, cancelledPayload{TargetOperationID: payload.TargetOperationID, Disposition: "cancelled"})
		}
		return p.sendCancelled(frame, cancelledPayload{TargetOperationID: payload.TargetOperationID, Disposition: "too_late"})
	}
	if payload.TargetMessageID == p.initializeMessageID {
		return p.sendCancelled(frame, cancelledPayload{TargetMessageID: payload.TargetMessageID, Disposition: "unsupported"})
	}
	if waiter := p.registry.CancelMessage(payload.TargetMessageID); waiter != nil {
		waiter.Cancel("cancelled")
		return p.sendCancelled(frame, cancelledPayload{TargetMessageID: payload.TargetMessageID, Disposition: "cancelled"})
	}
	return p.sendCancelled(frame, cancelledPayload{TargetMessageID: payload.TargetMessageID, Disposition: "too_late"})
}

func (p *Plugin) sendCancelled(frame ipc.Frame, payload cancelledPayload) error {
	return p.writer.Send(p.nextMessageID("c"), frame.MessageID, "plugin.cancelled", payload)
}

type shutdownPayload struct {
	GracePeriodMs int64 `json:"gracePeriodMs"`
}

type stoppedPayload struct {
	InFlightCount int `json:"inFlightCount"`
}

func (p *Plugin) handleShutdown(frame ipc.Frame) error {
	payload, err := ipc.DecodePayload[shutdownPayload](frame.Payload)
	if err != nil {
		return err
	}
	if payload.GracePeriodMs < 0 {
		return p.violation("SHUTDOWN_GRACE_INVALID", frame.Type)
	}
	p.shuttingDown.Store(true)
	grace := time.Duration(p.config.shutdownGrace()) * time.Millisecond
	if payload.GracePeriodMs > 0 {
		grace = time.Duration(payload.GracePeriodMs) * time.Millisecond
	}
	if p.server != nil {
		ctx, cancel := p.shutdownContext()
		_ = p.server.Shutdown(ctx)
		cancel()
	}
	live := p.registry.All()
	for _, waiter := range live {
		p.registry.Remove(waiter.ChannelID)
		waiter.Cancel("shutdown")
	}
	finished := make(chan struct{})
	go func() {
		p.inFlight.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(grace):
	}
	return p.writer.Send(p.nextMessageID("s"), frame.MessageID, "plugin.stopped", stoppedPayload{InFlightCount: len(live)})
}
