package staging_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/odango-chan/ca-openamsm-plugin/internal/slot"
	"github.com/odango-chan/ca-openamsm-plugin/internal/staging"
)

func newStore(t *testing.T) (*staging.Store, string, string) {
	t.Helper()
	inbound := t.TempDir()
	outbound := t.TempDir()
	store, err := staging.New(inbound, outbound)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store, inbound, outbound
}

func writeFile(t *testing.T, root, relative string, payload []byte) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestAC00902WriteInboundExactBytesAndDescriptor(t *testing.T) {
	store, inbound, _ := newStore(t)
	payload := []byte{0x2e, 0x05, 0x01, 0xc9, 0x99, 0x88, 0x77}
	descriptor, err := store.WriteInbound("evt-1", slot.N1Request, "n1SmMsg", slot.MediaTypeNAS, payload)
	if err != nil {
		t.Fatalf("write inbound: %v", err)
	}
	if descriptor.Slot != slot.N1Request || descriptor.MediaType != slot.MediaTypeNAS || descriptor.ContentID != "n1SmMsg" {
		t.Fatalf("descriptor = %+v", descriptor)
	}
	if descriptor.ByteLength != int64(len(payload)) {
		t.Fatalf("byteLength = %d", descriptor.ByteLength)
	}
	if descriptor.StagingPath != "event-evt-1/n1.bin" {
		t.Fatalf("stagingPath = %q", descriptor.StagingPath)
	}
	if filepath.IsAbs(descriptor.StagingPath) {
		t.Fatalf("stagingPath must be relative")
	}
	absolute, err := staging.ResolveWithin(inbound, descriptor.StagingPath)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	stored, err := os.ReadFile(absolute)
	if err != nil {
		t.Fatalf("read staged: %v", err)
	}
	if string(stored) != string(payload) {
		t.Fatalf("staged bytes differ: %v", stored)
	}
}

func TestAC00902InboundPathsUniquePerEvent(t *testing.T) {
	store, inbound, _ := newStore(t)
	first, err := store.WriteInbound("evt-1", slot.N2Request, "n2SmInfo", slot.MediaTypeNGAP, []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := store.WriteInbound("evt-2", slot.N2Request, "n2SmInfo", slot.MediaTypeNGAP, []byte{9, 8, 7})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.StagingPath == second.StagingPath {
		t.Fatalf("events must not share staging paths")
	}
	for _, descriptor := range []staging.Descriptor{first, second} {
		absolute, err := staging.ResolveWithin(inbound, descriptor.StagingPath)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if _, err := os.Stat(absolute); err != nil {
			t.Fatalf("stat: %v", err)
		}
	}
}

func TestAC00902RejectInboundRootMissing(t *testing.T) {
	if _, err := staging.New(filepath.Join(t.TempDir(), "missing"), t.TempDir()); err == nil {
		t.Fatalf("missing inbound root must fail")
	}
	if _, err := staging.New(t.TempDir(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatalf("missing outbound root must fail")
	}
}

func TestAC00902RejectInvalidEventIDAndPart(t *testing.T) {
	store, _, _ := newStore(t)
	if _, err := store.WriteInbound("../escape", slot.N1Request, "", slot.MediaTypeNAS, []byte{1}); err == nil {
		t.Fatalf("invalid event id must fail")
	}
	if _, err := store.WriteInbound("evt-1", slot.N1Request, "", slot.MediaTypeNGAP, []byte{1}); err == nil {
		t.Fatalf("wrong media type must fail")
	}
	if _, err := store.WriteInbound("evt-1", slot.N1Request, "", slot.MediaTypeNAS, nil); err == nil {
		t.Fatalf("empty part must fail")
	}
	if _, err := store.WriteInbound("evt-1", slot.N1Response, "", slot.MediaTypeNAS, []byte{1}); err == nil {
		t.Fatalf("outbound slot must not be written as inbound")
	}
}

func TestAC00903RejectAbsoluteAndTraversalOutboundPaths(t *testing.T) {
	store, _, outbound := newStore(t)
	writeFile(t, outbound, "ok/n1.bin", []byte{1})
	for _, path := range []string{
		"",
		"/etc/passwd",
		"C:/temp/n1.bin",
		"../outside.bin",
		"ok/../../outside.bin",
		`..\outside.bin`,
		"ok/n1.bin:stream",
	} {
		descriptor := staging.Descriptor{Slot: slot.N1Response, MediaType: slot.MediaTypeNAS, StagingPath: path, ByteLength: 1}
		if _, err := store.ReadOutbound(descriptor); err == nil {
			t.Fatalf("path %q must be rejected", path)
		}
	}
}

func TestAC00903RejectSymlinkEscape(t *testing.T) {
	store, _, outbound := newStore(t)
	outside := filepath.Join(t.TempDir(), "outside.bin")
	if err := os.WriteFile(outside, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	link := filepath.Join(outbound, "link.bin")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable on this host: %v", err)
	}
	descriptor := staging.Descriptor{Slot: slot.N1Response, MediaType: slot.MediaTypeNAS, StagingPath: "link.bin", ByteLength: 3}
	if _, err := store.ReadOutbound(descriptor); err == nil {
		t.Fatalf("symlink escape must be rejected")
	}
}

func TestAC00903RejectMissingEmptyAndByteLengthMismatch(t *testing.T) {
	store, _, outbound := newStore(t)
	writeFile(t, outbound, "empty.bin", nil)
	writeFile(t, outbound, "short.bin", []byte{1, 2, 3})

	cases := []staging.Descriptor{
		{Slot: slot.N1Response, MediaType: slot.MediaTypeNAS, StagingPath: "missing.bin", ByteLength: 3},
		{Slot: slot.N1Response, MediaType: slot.MediaTypeNAS, StagingPath: "empty.bin", ByteLength: 0},
		{Slot: slot.N1Response, MediaType: slot.MediaTypeNAS, StagingPath: "short.bin", ByteLength: 4},
	}
	for _, descriptor := range cases {
		if _, err := store.ReadOutbound(descriptor); err == nil {
			t.Fatalf("descriptor %+v must be rejected", descriptor)
		}
	}
}

func TestAC00903RejectUnknownSlotOrMediaType(t *testing.T) {
	store, _, outbound := newStore(t)
	writeFile(t, outbound, "n1.bin", []byte{1})
	cases := []staging.Descriptor{
		{Slot: slot.N1Request, MediaType: slot.MediaTypeNAS, StagingPath: "n1.bin", ByteLength: 1},
		{Slot: "n9.response", MediaType: slot.MediaTypeNAS, StagingPath: "n1.bin", ByteLength: 1},
		{Slot: slot.N1Response, MediaType: slot.MediaTypeNGAP, StagingPath: "n1.bin", ByteLength: 1},
	}
	for _, descriptor := range cases {
		if _, err := store.ReadOutbound(descriptor); err == nil {
			t.Fatalf("descriptor %+v must be rejected", descriptor)
		}
	}
}

func TestAC00903DescriptorExposesNoHostIdentity(t *testing.T) {
	store, _, _ := newStore(t)
	descriptor, err := store.WriteInbound("evt-1", slot.N1Request, "n1SmMsg", slot.MediaTypeNAS, []byte{1, 2, 3, 4})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	encoded, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	expected := map[string]bool{"slot": true, "mediaType": true, "contentId": true, "stagingPath": true, "byteLength": true}
	for key := range fields {
		if !expected[key] {
			t.Fatalf("descriptor leaked field %q", key)
		}
	}
	if len(fields) != len(expected) {
		t.Fatalf("descriptor fields = %v", fields)
	}
}

func TestAC00902RemoveEventCleansOnlyThatEvent(t *testing.T) {
	store, inbound, _ := newStore(t)
	if _, err := store.WriteInbound("evt-1", slot.N1Request, "", slot.MediaTypeNAS, []byte{1}); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if _, err := store.WriteInbound("evt-2", slot.N1Request, "", slot.MediaTypeNAS, []byte{2}); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	if err := store.RemoveEvent("evt-1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(inbound, "event-evt-1")); !os.IsNotExist(err) {
		t.Fatalf("event-1 directory still present")
	}
	if _, err := os.Stat(filepath.Join(inbound, "event-evt-2")); err != nil {
		t.Fatalf("event-2 directory must remain: %v", err)
	}
}
