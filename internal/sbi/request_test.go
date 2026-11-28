package sbi_test

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/odango-chan/ca-openamsm-plugin/internal/event"
	"github.com/odango-chan/ca-openamsm-plugin/internal/sbi"
	"github.com/odango-chan/ca-openamsm-plugin/internal/slot"
	"github.com/odango-chan/ca-openamsm-plugin/internal/testsupport"
)

const limit = 1 << 20

func makeRequest(t *testing.T, method, target, contentType string, body []byte) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return request
}

type testPart struct {
	contentType string
	contentID   string
	body        []byte
}

func multipartBody(t *testing.T, parts ...testPart) (string, []byte) {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, part := range parts {
		header := textproto.MIMEHeader{}
		if part.contentType != "" {
			header.Set("Content-Type", part.contentType)
		}
		if part.contentID != "" {
			header.Set("Content-ID", part.contentID)
		}
		target, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		if _, err := target.Write(part.body); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return "multipart/related; boundary=" + writer.Boundary(), buffer.Bytes()
}

func mustParse(t *testing.T, request *http.Request, maxBytes int64) event.Input {
	t.Helper()
	parsed, err := sbi.ParseRequest(request, maxBytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return parsed
}

func expectSBIError(t *testing.T, err error, code string) {
	t.Helper()
	var sbiError *sbi.Error
	if !errors.As(err, &sbiError) || sbiError.Code != code {
		t.Fatalf("err = %v, want code %s", err, code)
	}
}

func TestAC00902ParseCreateMultipart(t *testing.T) {
	contentType, body := testsupport.Multipart(testsupport.CreateJSON(),
		testsupport.BinaryPart{ContentID: "n1SmMsg", MediaType: testsupport.MediaNAS, Bytes: testsupport.N1Modify},
		testsupport.BinaryPart{ContentID: "n2SmInfo", MediaType: testsupport.MediaNGAP, Bytes: testsupport.N2SetupResponse},
	)
	parsed := mustParse(t, makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	if parsed.Operation != event.OperationCreate || parsed.SmContextRef != "" {
		t.Fatalf("operation = %q ref = %q", parsed.Operation, parsed.SmContextRef)
	}
	if string(parsed.Request) != string(testsupport.CreateJSON()) {
		t.Fatalf("request JSON was rewritten")
	}
	if parsed.N1 == nil || string(parsed.N1.Bytes) != string(testsupport.N1Modify) || parsed.N1.ContentID != "n1SmMsg" {
		t.Fatalf("N1 = %+v", parsed.N1)
	}
	if parsed.N2 == nil || string(parsed.N2.Bytes) != string(testsupport.N2SetupResponse) {
		t.Fatalf("N2 = %+v", parsed.N2)
	}
	if parsed.N2Type != "PDU_RES_SETUP_REQ" {
		t.Fatalf("N2Type = %q", parsed.N2Type)
	}
	if parsed.N2Ext1 != nil || parsed.N2Ext1Type != "" {
		t.Fatalf("unexpected Ext1")
	}
}

func TestAC00902ParseCreateWithN2Ext1SlotContract(t *testing.T) {
	request := []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"},"n2SmInfo":{"contentId":"n2SmInfo"},"n2SmInfoType":"PDU_RES_SETUP_REQ","n2SmInfoExt1":{"contentId":"n2SmInfoExt1"},"n2SmInfoTypeExt1":"PDU_RES_SETUP_FAIL"}`)
	contentType, body := testsupport.Multipart(request,
		testsupport.BinaryPart{ContentID: "n1SmMsg", MediaType: testsupport.MediaNAS, Bytes: testsupport.N1Modify},
		testsupport.BinaryPart{ContentID: "n2SmInfo", MediaType: testsupport.MediaNGAP, Bytes: testsupport.N2SetupResponse},
		testsupport.BinaryPart{ContentID: "n2SmInfoExt1", MediaType: testsupport.MediaNGAP, Bytes: testsupport.N2Ext1Response},
	)
	parsed := mustParse(t, makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	if parsed.N2Ext1 == nil || string(parsed.N2Ext1.Bytes) != string(testsupport.N2Ext1Response) {
		t.Fatalf("N2Ext1 = %+v", parsed.N2Ext1)
	}
	if parsed.N2Ext1Type != "PDU_RES_SETUP_FAIL" {
		t.Fatalf("N2Ext1Type = %q", parsed.N2Ext1Type)
	}
	normalized, err := event.Normalize(parsed, nil)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(normalized.Parts) != 3 || normalized.Parts[2].Slot != slot.N2Ext1Request {
		t.Fatalf("parts = %+v", normalized.Parts)
	}
}

func TestAC00902ParseUpdateMultipartOptionalN2(t *testing.T) {
	contentType, body := testsupport.Multipart(testsupport.UpdateJSONModify(),
		testsupport.BinaryPart{ContentID: "n1SmMsg", MediaType: testsupport.MediaNAS, Bytes: testsupport.N1Modify},
		testsupport.BinaryPart{ContentID: "n2SmInfo", MediaType: testsupport.MediaNGAP, Bytes: testsupport.N2ModResponse},
	)
	parsed := mustParse(t, makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts/ref-1/modify", contentType, body), limit)
	if parsed.Operation != event.OperationUpdate || parsed.SmContextRef != "ref-1" {
		t.Fatalf("operation = %q ref = %q", parsed.Operation, parsed.SmContextRef)
	}
	if parsed.N2 == nil || parsed.N2Type != "PDU_RES_MOD_REQ" {
		t.Fatalf("N2 = %+v type = %q", parsed.N2, parsed.N2Type)
	}
}

func TestAC00902NormalizeContentIDWithBoundsAndWhitespace(t *testing.T) {
	request := []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"}}`)
	contentType, body := multipartBody(t,
		testPart{contentType: "application/json", body: request},
		testPart{contentType: testsupport.MediaNAS, contentID: " <n1SmMsg> ", body: testsupport.N1Modify},
	)
	parsed := mustParse(t, makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	if parsed.N1 == nil || parsed.N1.ContentID != "n1SmMsg" {
		t.Fatalf("content-id not normalized: %+v", parsed.N1)
	}
}

func TestAC00902RejectMissingReferencedPart(t *testing.T) {
	contentType, body := multipartBody(t,
		testPart{contentType: "application/json", body: []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"}}`)},
	)
	_, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	expectSBIError(t, err, sbi.CodeContentIDUnresolved)
}

func TestAC00902RejectDuplicateContentID(t *testing.T) {
	contentType, body := multipartBody(t,
		testPart{contentType: "application/json", body: []byte(`{"n1SmMsg":{"contentId":"same"}}`)},
		testPart{contentType: testsupport.MediaNAS, contentID: "same", body: testsupport.N1Modify},
		testPart{contentType: testsupport.MediaNAS, contentID: "same", body: testsupport.N1Modify},
	)
	_, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	expectSBIError(t, err, sbi.CodeContentIDDuplicate)
}

func TestAC00902RejectUnreferencedPart(t *testing.T) {
	contentType, body := multipartBody(t,
		testPart{contentType: "application/json", body: []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"}}`)},
		testPart{contentType: testsupport.MediaNAS, contentID: "n1SmMsg", body: testsupport.N1Modify},
		testPart{contentType: testsupport.MediaNGAP, contentID: "extraN2", body: testsupport.N2SetupResponse},
	)
	_, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	expectSBIError(t, err, sbi.CodeContentIDUnreferenced)
}

func TestAC00902RejectWrongMediaType(t *testing.T) {
	contentType, body := multipartBody(t,
		testPart{contentType: "application/json", body: []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"}}`)},
		testPart{contentType: testsupport.MediaNGAP, contentID: "n1SmMsg", body: testsupport.N2SetupResponse},
	)
	_, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	expectSBIError(t, err, sbi.CodeMediaTypeMismatch)
}

func TestAC00902RejectDuplicateReferenceSamePart(t *testing.T) {
	contentType, body := multipartBody(t,
		testPart{contentType: "application/json", body: []byte(`{"n1SmMsg":{"contentId":"shared"},"n2SmInfo":{"contentId":"shared"}}`)},
		testPart{contentType: testsupport.MediaNAS, contentID: "shared", body: testsupport.N1Modify},
	)
	_, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	expectSBIError(t, err, sbi.CodeContentIDDuplicateRef)
}

func TestAC00902RejectMissingJSONPartAndDuplicateJSONPart(t *testing.T) {
	contentType, body := multipartBody(t,
		testPart{contentType: testsupport.MediaNAS, contentID: "n1SmMsg", body: testsupport.N1Modify},
	)
	_, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	expectSBIError(t, err, sbi.CodeJSONPartMissing)

	contentType, body = multipartBody(t,
		testPart{contentType: "application/json", body: []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"}}`)},
		testPart{contentType: "application/json", body: []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"}}`)},
		testPart{contentType: testsupport.MediaNAS, contentID: "n1SmMsg", body: testsupport.N1Modify},
	)
	_, err = sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	expectSBIError(t, err, sbi.CodeJSONPartDuplicate)
}

func TestAC00902RejectMissingPartContentID(t *testing.T) {
	contentType, body := multipartBody(t,
		testPart{contentType: "application/json", body: []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"}}`)},
		testPart{contentType: testsupport.MediaNAS, body: testsupport.N1Modify},
	)
	_, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	expectSBIError(t, err, sbi.CodePartContentIDMissing)
}

func TestAC00902RejectN2TypeWithoutPart(t *testing.T) {
	contentType, body := multipartBody(t,
		testPart{contentType: "application/json", body: []byte(`{"n1SmMsg":{"contentId":"n1"},"n2SmInfoType":"PDU_RES_MOD_REQ"}`)},
		testPart{contentType: testsupport.MediaNAS, contentID: "n1", body: testsupport.N1Modify},
	)
	_, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts/r/modify", contentType, body), limit)
	expectSBIError(t, err, sbi.CodeN2TypeWithoutPart)
}

func TestAC00902RejectCreateJSONOnlyAndUnsupportedTargets(t *testing.T) {
	_, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", "application/json", testsupport.CreateJSON()), limit)
	expectSBIError(t, err, sbi.CodeCreateRequiresMultipart)

	_, err = sbi.ParseRequest(makeRequest(t, http.MethodGet, "http://sbi.test/sm-contexts", "", nil), limit)
	expectSBIError(t, err, sbi.CodeMethodNotAllowed)

	_, err = sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts/r/release", "application/json", []byte(`{}`)), limit)
	expectSBIError(t, err, sbi.CodePathUnsupported)

	_, err = sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts//modify", "application/json", []byte(`{}`)), limit)
	expectSBIError(t, err, sbi.CodePathUnsupported)
}

func TestAC00902RejectInvalidJSONAndMalformedMultipart(t *testing.T) {
	contentType, body := multipartBody(t,
		testPart{contentType: "application/json", body: []byte(`{"n1SmMsg":`)},
		testPart{contentType: testsupport.MediaNAS, contentID: "n1SmMsg", body: testsupport.N1Modify},
	)
	_, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	expectSBIError(t, err, sbi.CodeJSONInvalid)

	_, err = sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", "multipart/related", []byte("no boundary")), limit)
	expectSBIError(t, err, sbi.CodeBoundaryMissing)

	_, err = sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", "text/plain", []byte("plain")), limit)
	expectSBIError(t, err, sbi.CodeMediaTypeUnsupported)
}

func TestAC00902RejectOversizedBody(t *testing.T) {
	contentType, body := multipartBody(t,
		testPart{contentType: "application/json", body: []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"},"pad":"` + strings.Repeat("x", 256) + `"}`)},
		testPart{contentType: testsupport.MediaNAS, contentID: "n1SmMsg", body: testsupport.N1Modify},
	)
	_, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), 128)
	expectSBIError(t, err, sbi.CodeBodyTooLarge)
}

func TestAC00902RejectEmptyBinaryPart(t *testing.T) {
	contentType, body := multipartBody(t,
		testPart{contentType: "application/json", body: []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"}}`)},
		testPart{contentType: testsupport.MediaNAS, contentID: "n1SmMsg", body: nil},
	)
	_, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts", contentType, body), limit)
	expectSBIError(t, err, sbi.CodePartEmpty)
}

func TestAC00902UpdateJSONWithoutBinaryFailsNormalization(t *testing.T) {
	parsed, err := sbi.ParseRequest(makeRequest(t, http.MethodPost, "http://sbi.test/sm-contexts/r/modify", "application/json", testsupport.UpdateJSONModify()), limit)
	if err != nil {
		t.Fatalf("json-only Update parse should succeed before classification: %v", err)
	}
	if parsed.N1 != nil || parsed.N2 != nil {
		t.Fatalf("json-only Update must have no binary parts")
	}
	if _, err := event.Normalize(parsed, event.TestDoubleClassifier{}); err == nil {
		t.Fatalf("json-only Update without N1 must fail normalization")
	}
}
