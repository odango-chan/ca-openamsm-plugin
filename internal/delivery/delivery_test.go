package delivery_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/odango-chan/ca-openamsm-plugin/internal/delivery"
	"github.com/odango-chan/ca-openamsm-plugin/internal/ipc"
)

func TestAC00905ClaimTripleBinding(t *testing.T) {
	registry := delivery.NewRegistry()
	waiter, err := registry.Register("evt-1", "nsmf.create-sm-context")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if waiter.ChannelID == "" || strings.HasPrefix(waiter.ChannelID, "evt-") {
		t.Fatalf("channel id = %q", waiter.ChannelID)
	}
	claimed, reason := registry.Claim(waiter.ChannelID, "nsmf.create-sm-context")
	if claimed != waiter || reason != "" {
		t.Fatalf("claim = %v reason=%q", claimed, reason)
	}
	if _, reason := registry.Claim(waiter.ChannelID, "nsmf.create-sm-context"); reason != "UNKNOWN_RESPONSE_CHANNEL" {
		t.Fatalf("second claim must be rejected, reason = %q", reason)
	}
}

func TestAC00905WrongEventTypeDoesNotConsumeChannel(t *testing.T) {
	registry := delivery.NewRegistry()
	waiter, _ := registry.Register("evt-1", "nsmf.update-sm-context")
	if _, reason := registry.Claim(waiter.ChannelID, "nsmf.create-sm-context"); reason != "EVENT_TYPE_MISMATCH" {
		t.Fatalf("reason = %q", reason)
	}
	if registry.Count() != 1 {
		t.Fatalf("mismatched dispatch must not consume the waiter")
	}
	if _, reason := registry.Claim(waiter.ChannelID, "nsmf.update-sm-context"); reason != "" {
		t.Fatalf("correct dispatch must still succeed, reason = %q", reason)
	}
}

func TestAC00905UnknownChannelRejected(t *testing.T) {
	registry := delivery.NewRegistry()
	if _, reason := registry.Claim("ch-unknown", "nsmf.create-sm-context"); reason != "UNKNOWN_RESPONSE_CHANNEL" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestAC00905CrossStreamIsolation(t *testing.T) {
	registry := delivery.NewRegistry()
	first, _ := registry.Register("evt-1", "nsmf.create-sm-context")
	second, _ := registry.Register("evt-2", "nsmf.create-sm-context")
	if first.ChannelID == second.ChannelID {
		t.Fatalf("channels must be unique")
	}
	claimedFirst, _ := registry.Claim(first.ChannelID, "nsmf.create-sm-context")
	claimedSecond, _ := registry.Claim(second.ChannelID, "nsmf.create-sm-context")
	if claimedFirst != first || claimedSecond != second {
		t.Fatalf("cross-stream claim mixed waiters")
	}
	if first.EventID != "evt-1" || second.EventID != "evt-2" {
		t.Fatalf("event ids mixed: %q %q", first.EventID, second.EventID)
	}
}

func TestAC00905ResultPayloadHasNoHostIdentity(t *testing.T) {
	cases := []delivery.Result{
		delivery.Delivered(201),
		delivery.Failed("RESPONSE_CHANNEL_MISSING", "no live channel"),
	}
	for _, result := range cases {
		encoded, err := json.Marshal(result.Payload("del-1"))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		allowed := map[string]bool{"deliveryId": true, "disposition": true, "statusCode": true, "error": true}
		for key := range fields {
			if !allowed[key] {
				t.Fatalf("delivery.result leaked %q", key)
			}
		}
		if fields["deliveryId"] != "del-1" {
			t.Fatalf("deliveryId = %v", fields["deliveryId"])
		}
		if result.Disposition == delivery.DispositionDelivered && fields["statusCode"] != float64(201) {
			t.Fatalf("statusCode = %v", fields["statusCode"])
		}
	}
}

func TestAC00905WaiterStoresNoHostIdentity(t *testing.T) {
	waiterType := reflect.TypeOf(delivery.Waiter{})
	for index := 0; index < waiterType.NumField(); index++ {
		name := waiterType.Field(index).Name
		for _, forbidden := range []string{"Obligation", "RunID", "SkillID", "ArtifactRef", "DeliveryID"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("Waiter must not store Host identity field %q", name)
			}
		}
	}
}

func TestAC00905AckAllowlistAndDisposition(t *testing.T) {
	good := []byte(`{"eventId":"evt-1","disposition":"accepted","error":{"code":"OK","message":"safe","retryable":false}}`)
	if _, err := ipc.DecodePayload[delivery.Ack](good); err != nil {
		t.Fatalf("good ack rejected: %v", err)
	}
	for _, forbidden := range []string{"runId", "skillId", "artifactRef", "responseObligationId", "deliveryId"} {
		payload := []byte(`{"eventId":"evt-1","disposition":"accepted","` + forbidden + `":"leak"}`)
		if _, err := ipc.DecodePayload[delivery.Ack](payload); err == nil {
			t.Fatalf("ack with %s must be rejected", forbidden)
		}
	}
	for _, disposition := range []string{delivery.AckAccepted, delivery.AckDuplicateAttached, delivery.AckConflict, delivery.AckRejected} {
		if !delivery.ValidAckDisposition(disposition) {
			t.Fatalf("%q must be valid", disposition)
		}
	}
	for _, disposition := range []string{"", "ok", "delivered"} {
		if delivery.ValidAckDisposition(disposition) {
			t.Fatalf("%q must be invalid", disposition)
		}
	}
}

func TestAC00905ConcurrentClaimExactlyOnce(t *testing.T) {
	registry := delivery.NewRegistry()
	waiters := make([]*delivery.Waiter, 16)
	for index := range waiters {
		waiter, err := registry.Register(string(rune('a'+index)), "nsmf.create-sm-context")
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		waiters[index] = waiter
	}
	var wait sync.WaitGroup
	successes := make(chan string, len(waiters)*2)
	for _, waiter := range waiters {
		for attempt := 0; attempt < 2; attempt++ {
			wait.Add(1)
			go func(channelID string) {
				defer wait.Done()
				if claimed, _ := registry.Claim(channelID, "nsmf.create-sm-context"); claimed != nil {
					successes <- channelID
				}
			}(waiter.ChannelID)
		}
	}
	wait.Wait()
	close(successes)
	seen := map[string]int{}
	for channelID := range successes {
		seen[channelID]++
	}
	if len(seen) != len(waiters) {
		t.Fatalf("claimed channels = %d, want %d", len(seen), len(waiters))
	}
	for channelID, count := range seen {
		if count != 1 {
			t.Fatalf("channel %s claimed %d times", channelID, count)
		}
	}
}

func TestAC00905CancelEventAndMessage(t *testing.T) {
	registry := delivery.NewRegistry()
	waiter, _ := registry.Register("evt-1", "nsmf.create-sm-context")
	if err := registry.BindIngressMessage(waiter.ChannelID, "m-1"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if cancelled := registry.CancelEvent("evt-1"); cancelled != waiter {
		t.Fatalf("cancel event = %v", cancelled)
	}
	if registry.Count() != 0 {
		t.Fatalf("waiter must be removed")
	}

	second, _ := registry.Register("evt-2", "nsmf.create-sm-context")
	if err := registry.BindIngressMessage(second.ChannelID, "m-2"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if cancelled := registry.CancelMessage("m-2"); cancelled != second {
		t.Fatalf("cancel message = %v", cancelled)
	}
	if registry.CancelEvent("evt-404") != nil {
		t.Fatalf("unknown cancel must return nil")
	}
}
