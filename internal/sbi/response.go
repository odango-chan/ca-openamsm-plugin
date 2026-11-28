package sbi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/odango-chan/ca-openamsm-plugin/internal/slot"
)

// OutboundPart is one Host-staged outbound binary part.
type OutboundPart struct {
	Slot      string
	MediaType string
	ContentID string
	Bytes     []byte
}

// Error codes produced by response serialization.
const (
	CodeResponseStatusInvalid   = "SBI_RESPONSE_STATUS_INVALID"
	CodeResponseBodyInvalid     = "SBI_RESPONSE_BODY_INVALID"
	CodeResponseArtifactInvalid = "SBI_RESPONSE_ARTIFACT_INVALID"
	CodeResponseWriteFailed     = "SBI_RESPONSE_WRITE_FAILED"
)

// BuildResponse serializes a Host-supplied SBI response. The Plugin does not
// invent a status code, resource URI or business body.
func BuildResponse(statusCode int, headers map[string]string, body json.RawMessage, parts []OutboundPart) (http.Header, []byte, error) {
	if statusCode < 100 || statusCode > 599 {
		return nil, nil, &Error{Code: CodeResponseStatusInvalid, Detail: "status"}
	}
	header := http.Header{}
	for name, value := range headers {
		if isForbiddenResponseHeader(name) {
			continue
		}
		header.Set(name, value)
	}
	trimmedBody := bytes.TrimSpace(body)
	if bytes.Equal(trimmedBody, []byte("null")) {
		// IPC v1alpha1 uses JSON null for "no body".
		trimmedBody = nil
	}
	if len(trimmedBody) > 0 && !json.Valid(trimmedBody) {
		return nil, nil, &Error{Code: CodeResponseBodyInvalid}
	}
	if len(parts) > 0 {
		if len(trimmedBody) == 0 {
			return nil, nil, &Error{Code: CodeResponseBodyInvalid, Detail: "multipart response requires a JSON data part"}
		}
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		jsonHeader := textproto.MIMEHeader{}
		jsonHeader.Set("Content-Type", "application/json")
		jsonPart, err := writer.CreatePart(jsonHeader)
		if err != nil {
			return nil, nil, &Error{Code: CodeResponseWriteFailed, Err: err}
		}
		if _, err := jsonPart.Write(trimmedBody); err != nil {
			return nil, nil, &Error{Code: CodeResponseWriteFailed, Err: err}
		}
		for _, part := range parts {
			expected, ok := slot.MediaType(part.Slot)
			if !ok || !slot.IsOutbound(part.Slot) {
				return nil, nil, &Error{Code: CodeResponseArtifactInvalid, Detail: part.Slot}
			}
			if part.MediaType != expected {
				return nil, nil, &Error{Code: CodeResponseArtifactInvalid, Detail: part.Slot + "/" + part.MediaType}
			}
			if len(part.Bytes) == 0 {
				return nil, nil, &Error{Code: CodeResponseArtifactInvalid, Detail: part.Slot + " empty"}
			}
			partHeader := textproto.MIMEHeader{}
			partHeader.Set("Content-Type", part.MediaType)
			if part.ContentID != "" {
				partHeader.Set("Content-ID", "<"+part.ContentID+">")
			}
			binaryPart, err := writer.CreatePart(partHeader)
			if err != nil {
				return nil, nil, &Error{Code: CodeResponseWriteFailed, Err: err}
			}
			if _, err := binaryPart.Write(part.Bytes); err != nil {
				return nil, nil, &Error{Code: CodeResponseWriteFailed, Err: err}
			}
		}
		if err := writer.Close(); err != nil {
			return nil, nil, &Error{Code: CodeResponseWriteFailed, Err: err}
		}
		header.Set("Content-Type", "multipart/related; boundary="+writer.Boundary())
		header.Set("Content-Length", strconv.Itoa(buffer.Len()))
		return header, buffer.Bytes(), nil
	}
	if len(trimmedBody) > 0 {
		header.Set("Content-Type", "application/json")
		header.Set("Content-Length", strconv.Itoa(len(trimmedBody)))
		return header, trimmedBody, nil
	}
	header.Set("Content-Length", "0")
	return header, nil, nil
}

// WriteResponse writes a built response to the live HTTP/2 stream.
func WriteResponse(w http.ResponseWriter, statusCode int, headers map[string]string, body json.RawMessage, parts []OutboundPart) error {
	header, payload, err := BuildResponse(statusCode, headers, body, parts)
	if err != nil {
		return err
	}
	for name, values := range header {
		for _, value := range values {
			w.Header().Set(name, value)
		}
	}
	w.WriteHeader(statusCode)
	if len(payload) == 0 {
		return nil
	}
	if _, err := w.Write(payload); err != nil {
		return &Error{Code: CodeResponseWriteFailed, Err: err}
	}
	return nil
}

func isForbiddenResponseHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "content-type", "content-length", "transfer-encoding", "connection",
		"keep-alive", "proxy-connection", "te", "trailer", "upgrade":
		return true
	}
	return false
}
