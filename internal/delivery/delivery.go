// Package delivery owns the Plugin-local response-channel waiters and the
// Delivery identity contract. Host-owned identities (responseObligationId,
// Run, Skill, artifactRef) are never stored on a waiter.
package delivery

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// Ack dispositions from IPC v1alpha1.
const (
	AckAccepted          = "accepted"
	AckDuplicateAttached = "duplicate_attached"
	AckConflict          = "conflict"
	AckRejected          = "rejected"
)

// Delivery dispositions from IPC v1alpha1.
const (
	DispositionDelivered = "delivered"
	DispositionFailed    = "failed"
	DispositionUnknown   = "unknown"
)

// SafeError is the IPC error object. Details carries only safe diagnostics.
type SafeError struct {
	Code      string          `json:"code"`
	Message   string          `json:"message"`
	Retryable bool            `json:"retryable"`
	Details   json.RawMessage `json:"details,omitempty"`
}

// Ack is the closed ingress.ack payload. The struct defines the allowlist: any
// Host identity (runId, skillId, artifactRef, responseObligationId, deliveryId)
// fails strict decoding.
type Ack struct {
	EventID     string     `json:"eventId"`
	Disposition string     `json:"disposition"`
	Error       *SafeError `json:"error,omitempty"`
}

// ValidAckDisposition reports whether the value belongs to the frozen set.
func ValidAckDisposition(value string) bool {
	switch value {
	case AckAccepted, AckDuplicateAttached, AckConflict, AckRejected:
		return true
	}
	return false
}

// OutboundArtifact is one Host-staged delivery output descriptor.
type OutboundArtifact struct {
	Slot        string `json:"slot"`
	MediaType   string `json:"mediaType"`
	ContentID   string `json:"contentId,omitempty"`
	StagingPath string `json:"stagingPath"`
	ByteLength  int64  `json:"byteLength"`
}

// Dispatch is the closed delivery.dispatch payload.
type Dispatch struct {
	DeliveryID           string             `json:"deliveryId"`
	ResponseObligationID string             `json:"responseObligationId"`
	ResponseChannelID    string             `json:"responseChannelId"`
	EventType            string             `json:"eventType"`
	StatusCode           int                `json:"statusCode"`
	Headers              map[string]string  `json:"headers"`
	Body                 json.RawMessage    `json:"body"`
	Artifacts            []OutboundArtifact `json:"artifacts"`
}

// Result is the internal Delivery outcome for one dispatch.
type Result struct {
	Disposition string
	StatusCode  *int
	Error       *SafeError
}

// Delivered builds a delivered result.
func Delivered(statusCode int) Result {
	value := statusCode
	return Result{Disposition: DispositionDelivered, StatusCode: &value}
}

// Failed builds a failed result with safe diagnostics.
func Failed(code, message string) Result {
	return Result{Disposition: DispositionFailed, Error: &SafeError{Code: code, Message: message, Retryable: false}}
}

// ResultPayload is the closed delivery.result payload. It has no field for
// responseObligationId, responseChannelId, runId or artifactRef.
type ResultPayload struct {
	DeliveryID  string     `json:"deliveryId"`
	Disposition string     `json:"disposition"`
	StatusCode  *int       `json:"statusCode,omitempty"`
	Error       *SafeError `json:"error,omitempty"`
}

// Payload projects a Result for the IPC frame.
func (r Result) Payload(deliveryID string) ResultPayload {
	return ResultPayload{DeliveryID: deliveryID, Disposition: r.Disposition, StatusCode: r.StatusCode, Error: r.Error}
}

// Waiter is the in-memory response waiter for one live HTTP/2 stream.
type Waiter struct {
	ChannelID        string
	EventID          string
	EventType        string
	IngressMessageID string
	AckCh            chan Ack
	DispatchCh       chan Dispatch
	ResultCh         chan Result
	CancelCh         chan string
	Created          time.Time
	consumed         bool
}

// Cancel signals that the live request must be abandoned.
func (w *Waiter) Cancel(reason string) {
	select {
	case w.CancelCh <- reason:
	default:
	}
}

// Registry routes ACKs, dispatches and cancels to Plugin-local waiters. It
// never holds Host-owned obligation or Run identity.
type Registry struct {
	mu        sync.Mutex
	byChannel map[string]*Waiter
	byIngress map[string]*Waiter
	byEvent   map[string]*Waiter
}

// NewRegistry builds an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		byChannel: map[string]*Waiter{},
		byIngress: map[string]*Waiter{},
		byEvent:   map[string]*Waiter{},
	}
}

// Register creates a waiter with fresh Plugin-local identifiers.
func (r *Registry) Register(eventID, eventType string) (*Waiter, error) {
	channelID, err := randomID("ch-")
	if err != nil {
		return nil, err
	}
	waiter := &Waiter{
		ChannelID:  channelID,
		EventID:    eventID,
		EventType:  eventType,
		AckCh:      make(chan Ack, 1),
		DispatchCh: make(chan Dispatch, 1),
		ResultCh:   make(chan Result, 1),
		CancelCh:   make(chan string, 1),
		Created:    time.Now(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byChannel[channelID] = waiter
	r.byEvent[eventID] = waiter
	return waiter, nil
}

// BindIngressMessage records the outbound ingress.event messageId so the ACK
// correlation can be resolved.
func (r *Registry) BindIngressMessage(channelID, messageID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	waiter, ok := r.byChannel[channelID]
	if !ok {
		return errUnknownChannel
	}
	waiter.IngressMessageID = messageID
	r.byIngress[messageID] = waiter
	return nil
}

// ByIngressMessage resolves an ingress.ack correlationId.
func (r *Registry) ByIngressMessage(messageID string) *Waiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byIngress[messageID]
}

// Claim atomically consumes the waiter for a dispatch when the event type
// matches. A nil waiter means the channel is unknown or already consumed.
func (r *Registry) Claim(channelID, eventType string) (*Waiter, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	waiter, ok := r.byChannel[channelID]
	if !ok || waiter.consumed {
		return nil, "UNKNOWN_RESPONSE_CHANNEL"
	}
	if waiter.EventType != eventType {
		return nil, "EVENT_TYPE_MISMATCH"
	}
	waiter.consumed = true
	r.removeLocked(waiter)
	return waiter, ""
}

// CancelEvent consumes a waiter by Event ID for host.cancel.
func (r *Registry) CancelEvent(eventID string) *Waiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	waiter, ok := r.byEvent[eventID]
	if !ok {
		return nil
	}
	waiter.consumed = true
	r.removeLocked(waiter)
	return waiter
}

// CancelMessage consumes a waiter by its ingress.event messageId.
func (r *Registry) CancelMessage(messageID string) *Waiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	waiter, ok := r.byIngress[messageID]
	if !ok {
		return nil
	}
	waiter.consumed = true
	r.removeLocked(waiter)
	return waiter
}

// Remove drops a waiter from all indexes.
func (r *Registry) Remove(channelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if waiter, ok := r.byChannel[channelID]; ok {
		r.removeLocked(waiter)
	}
}

// All returns a snapshot for shutdown cancellation.
func (r *Registry) All() []*Waiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	waiters := make([]*Waiter, 0, len(r.byChannel))
	for _, waiter := range r.byChannel {
		waiters = append(waiters, waiter)
	}
	return waiters
}

// Count returns live waiters.
func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byChannel)
}

func (r *Registry) removeLocked(waiter *Waiter) {
	delete(r.byChannel, waiter.ChannelID)
	delete(r.byEvent, waiter.EventID)
	if waiter.IngressMessageID != "" {
		delete(r.byIngress, waiter.IngressMessageID)
	}
}

var errUnknownChannel = &RegistryError{Code: "REGISTRY_UNKNOWN_CHANNEL"}

// RegistryError is a stable registry diagnostic.
type RegistryError struct{ Code string }

func (e *RegistryError) Error() string { return e.Code }

func randomID(prefix string) (string, error) {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buffer), nil
}
