package sbi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/odango-chan/ca-openamsm-plugin/internal/sbi"
	"github.com/odango-chan/ca-openamsm-plugin/internal/slot"
	"github.com/odango-chan/ca-openamsm-plugin/internal/testsupport"
)

func TestAC00905ResponseMultipartContentIDAndByteFidelity(t *testing.T) {
	header, payload, err := sbi.BuildResponse(201, map[string]string{
		"Location":       "https://smf.example.test/nsmf-pdusession/v1/sm-contexts/1",
		"Content-Type":   "application/json",
		"Content-Length": "1",
	}, testsupport.Response201Body(), []sbi.OutboundPart{{
		Slot:      slot.N1Response,
		MediaType: slot.MediaTypeNAS,
		ContentID: "n1SmMsg",
		Bytes:     testsupport.N1OwnerResponse,
	}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	mediaType, parameters, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/related" || parameters["boundary"] == "" {
		t.Fatalf("content type = %q err=%v", header.Get("Content-Type"), err)
	}
	if header.Get("Location") == "" {
		t.Fatalf("Host Location header lost")
	}
	if header.Get("Content-Length") == "1" {
		t.Fatalf("Host-supplied Content-Length must be replaced")
	}
	reader := multipart.NewReader(bytes.NewReader(payload), parameters["boundary"])
	jsonPart, err := reader.NextPart()
	if err != nil {
		t.Fatalf("json part: %v", err)
	}
	jsonBytes, _ := io.ReadAll(jsonPart)
	if !bytes.Equal(jsonBytes, testsupport.Response201Body()) {
		t.Fatalf("JSON body rewritten")
	}
	binaryPart, err := reader.NextPart()
	if err != nil {
		t.Fatalf("binary part: %v", err)
	}
	if binaryPart.Header.Get("Content-ID") != "<n1SmMsg>" {
		t.Fatalf("Content-ID = %q", binaryPart.Header.Get("Content-ID"))
	}
	if binaryPart.Header.Get("Content-Type") != slot.MediaTypeNAS {
		t.Fatalf("part media type = %q", binaryPart.Header.Get("Content-Type"))
	}
	binaryBytes, _ := io.ReadAll(binaryPart)
	if !bytes.Equal(binaryBytes, testsupport.N1OwnerResponse) {
		t.Fatalf("N1 response bytes changed")
	}
	if _, err := reader.NextPart(); err != io.EOF {
		t.Fatalf("unexpected extra part: %v", err)
	}
}

func TestAC00905ResponseJSONOnlyAndNoBody(t *testing.T) {
	header, payload, err := sbi.BuildResponse(200, map[string]string{"Content-Type": "text/plain"}, testsupport.Response201Body(), nil)
	if err != nil {
		t.Fatalf("build json: %v", err)
	}
	if header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", header.Get("Content-Type"))
	}
	if !bytes.Equal(payload, testsupport.Response201Body()) {
		t.Fatalf("body rewritten")
	}

	header, payload, err = sbi.BuildResponse(204, nil, nil, nil)
	if err != nil {
		t.Fatalf("build empty: %v", err)
	}
	if len(payload) != 0 || header.Get("Content-Length") != "0" {
		t.Fatalf("empty response = %q %q", payload, header.Get("Content-Length"))
	}
}

func TestAC00905ResponseForbiddenAndHopByHopHeadersDropped(t *testing.T) {
	header, _, err := sbi.BuildResponse(200, map[string]string{
		"Transfer-Encoding": "chunked",
		"Connection":        "keep-alive",
		"Cache-Control":     "no-store",
	}, json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if header.Get("Transfer-Encoding") != "" || header.Get("Connection") != "" {
		t.Fatalf("hop-by-hop headers must be dropped")
	}
	if header.Get("Cache-Control") != "no-store" {
		t.Fatalf("safe Host header lost")
	}
}

func TestAC00905ResponseRejectsInvalidHostInput(t *testing.T) {
	if _, _, err := sbi.BuildResponse(99, nil, nil, nil); err == nil {
		t.Fatalf("invalid status must fail")
	}
	if _, _, err := sbi.BuildResponse(200, nil, json.RawMessage(`{broken`), nil); err == nil {
		t.Fatalf("invalid JSON body must fail")
	}
	if _, _, err := sbi.BuildResponse(200, nil, nil, []sbi.OutboundPart{{Slot: slot.N1Response, MediaType: slot.MediaTypeNAS, Bytes: []byte{1}}}); err == nil {
		t.Fatalf("multipart without JSON data part must fail")
	}
	if _, _, err := sbi.BuildResponse(200, nil, json.RawMessage(`{}`), []sbi.OutboundPart{{
		Slot: slot.N1Response, MediaType: slot.MediaTypeNGAP, Bytes: []byte{1},
	}}); err == nil {
		t.Fatalf("slot/media mismatch must fail")
	}
	if _, _, err := sbi.BuildResponse(200, nil, json.RawMessage(`{}`), []sbi.OutboundPart{{
		Slot: slot.N1Request, MediaType: slot.MediaTypeNAS, Bytes: []byte{1},
	}}); err == nil {
		t.Fatalf("inbound slot must not be delivered")
	}
}

func TestAC00905WriteResponseToLiveStream(t *testing.T) {
	recorder := httptest.NewRecorder()
	err := sbi.WriteResponse(recorder, 201, map[string]string{"Location": "https://smf.example.test/x"}, testsupport.Response201Body(), nil)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if recorder.Code != 201 {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if recorder.Header().Get("Location") == "" {
		t.Fatalf("Location lost")
	}
	if !bytes.Equal(recorder.Body.Bytes(), testsupport.Response201Body()) {
		t.Fatalf("body changed")
	}
}

var _ = http.MethodPost
