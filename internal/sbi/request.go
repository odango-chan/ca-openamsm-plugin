// Package sbi implements OpenAMSM SBI ingress parsing and response
// serialization. It owns HTTP wire handling, multipart/related parsing and
// Content-ID association only; it does not decode NAS/NGAP payloads.
package sbi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/odango-chan/ca-openamsm-plugin/internal/event"
	"github.com/odango-chan/ca-openamsm-plugin/internal/slot"
)

// Error codes produced by SBI parsing.
const (
	CodeMethodNotAllowed          = "SBI_METHOD_NOT_ALLOWED"
	CodePathUnsupported           = "SBI_PATH_UNSUPPORTED"
	CodeMediaTypeUnsupported      = "SBI_MEDIA_TYPE_UNSUPPORTED"
	CodeCreateRequiresMultipart   = "SBI_CREATE_REQUIRES_MULTIPART"
	CodeBodyTooLarge              = "SBI_BODY_TOO_LARGE"
	CodeBoundaryMissing           = "SBI_BOUNDARY_MISSING"
	CodeJSONPartMissing           = "SBI_JSON_PART_MISSING"
	CodeJSONPartDuplicate         = "SBI_JSON_PART_DUPLICATE"
	CodeJSONInvalid               = "SBI_JSON_INVALID"
	CodePartContentIDMissing      = "SBI_PART_CONTENT_ID_MISSING"
	CodeContentIDDuplicate        = "SBI_CONTENT_ID_DUPLICATE"
	CodeContentIDUnresolved       = "SBI_CONTENT_ID_UNRESOLVED"
	CodeContentIDUnreferenced     = "SBI_CONTENT_ID_UNREFERENCED"
	CodeContentIDDuplicateRef     = "SBI_CONTENT_ID_DUPLICATE_REFERENCE"
	CodeReferenceContentIDMissing = "SBI_REFERENCE_CONTENT_ID_MISSING"
	CodeMediaTypeMismatch         = "SBI_MEDIA_TYPE_MISMATCH"
	CodePartMediaTypeUnsupported  = "SBI_PART_MEDIA_TYPE_UNSUPPORTED"
	CodePartEmpty                 = "SBI_PART_EMPTY"
	CodeN2TypeWithoutPart         = "SBI_N2_TYPE_WITHOUT_PART"
	CodeN2Ext1TypeWithoutPart     = "SBI_N2_EXT1_TYPE_WITHOUT_PART"
	CodeSmContextRefInvalid       = "SBI_SM_CONTEXT_REF_INVALID"
)

// Error is an SBI parse failure with a stable code.
type Error struct {
	Code   string
	Detail string
	Err    error
}

func (e *Error) Error() string {
	message := e.Code
	if e.Detail != "" {
		message += ": " + e.Detail
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

func (e *Error) Unwrap() error { return e.Err }

// MaxPartBytes is the per-part safety cap (the request cap is enforced by the
// HTTP handler through http.MaxBytesReader).
const MaxPartBytes = 16 << 20

type rawPart struct {
	contentID string
	mediaType string
	bytes     []byte
}

// ParseRequest maps one HTTP request to the pre-classification Event input.
// It resolves Content-ID references and validates multipart part/slot media
// types. It never rewrites the JSON request object.
func ParseRequest(r *http.Request, maxRequestBytes int64) (event.Input, error) {
	operation, smContextRef, err := parseTarget(r)
	if err != nil {
		return event.Input{}, err
	}

	contentType := r.Header.Get("Content-Type")
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		return event.Input{}, &Error{Code: CodeMediaTypeUnsupported, Detail: contentType}
	}

	if mediaType == "application/json" {
		if operation == event.OperationCreate {
			return event.Input{}, &Error{Code: CodeCreateRequiresMultipart}
		}
		request, err := readLimited(r.Body, maxRequestBytes)
		if err != nil {
			return event.Input{}, err
		}
		if !json.Valid(request) {
			return event.Input{}, &Error{Code: CodeJSONInvalid}
		}
		return event.Input{Operation: operation, SmContextRef: smContextRef, Request: request}, nil
	}
	if mediaType != "multipart/related" {
		return event.Input{}, &Error{Code: CodeMediaTypeUnsupported, Detail: mediaType}
	}
	boundary := parameters["boundary"]
	if boundary == "" {
		return event.Input{}, &Error{Code: CodeBoundaryMissing}
	}

	request, binaries, err := parseMultipart(r.Body, boundary, maxRequestBytes)
	if err != nil {
		return event.Input{}, err
	}
	return resolveReferences(operation, smContextRef, request, binaries)
}

func parseTarget(r *http.Request) (string, string, error) {
	if r.Method != http.MethodPost {
		return "", "", &Error{Code: CodeMethodNotAllowed, Detail: r.Method}
	}
	escaped := r.URL.EscapedPath()
	if escaped == "/sm-contexts" {
		return event.OperationCreate, "", nil
	}
	const prefix = "/sm-contexts/"
	const suffix = "/modify"
	if strings.HasPrefix(escaped, prefix) && strings.HasSuffix(escaped, suffix) {
		rawRef := strings.TrimSuffix(strings.TrimPrefix(escaped, prefix), suffix)
		if rawRef == "" || strings.Contains(rawRef, "/") {
			return "", "", &Error{Code: CodePathUnsupported, Detail: escaped}
		}
		ref, err := url.PathUnescape(rawRef)
		if err != nil || strings.TrimSpace(ref) == "" || len(ref) > 512 || strings.ContainsAny(ref, "\r\n\x00") {
			return "", "", &Error{Code: CodeSmContextRefInvalid, Detail: rawRef}
		}
		return event.OperationUpdate, ref, nil
	}
	return "", "", &Error{Code: CodePathUnsupported, Detail: escaped}
}

func parseMultipart(body io.Reader, boundary string, maxRequestBytes int64) (json.RawMessage, []rawPart, error) {
	reader := newMultipartReader(body, boundary)
	var request json.RawMessage
	var binaries []rawPart
	seenContentID := map[string]struct{}{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, &Error{Code: CodeMediaTypeUnsupported, Detail: "multipart: " + err.Error(), Err: err}
		}
		partType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil || partType == "" {
			return nil, nil, &Error{Code: CodePartMediaTypeUnsupported, Detail: part.Header.Get("Content-Type")}
		}
		payload, err := readLimited(part, maxRequestBytes)
		if err != nil {
			return nil, nil, err
		}
		if partType == "application/json" {
			if request != nil {
				return nil, nil, &Error{Code: CodeJSONPartDuplicate}
			}
			if !json.Valid(payload) {
				return nil, nil, &Error{Code: CodeJSONInvalid}
			}
			request = payload
			continue
		}
		if partType != slot.MediaTypeNAS && partType != slot.MediaTypeNGAP {
			return nil, nil, &Error{Code: CodePartMediaTypeUnsupported, Detail: partType}
		}
		if len(payload) == 0 {
			return nil, nil, &Error{Code: CodePartEmpty, Detail: partType}
		}
		contentID := normalizeContentID(part.Header.Get("Content-ID"))
		if contentID == "" {
			return nil, nil, &Error{Code: CodePartContentIDMissing}
		}
		if _, duplicate := seenContentID[contentID]; duplicate {
			return nil, nil, &Error{Code: CodeContentIDDuplicate, Detail: contentID}
		}
		seenContentID[contentID] = struct{}{}
		binaries = append(binaries, rawPart{contentID: contentID, mediaType: partType, bytes: payload})
	}
	if request == nil {
		return nil, nil, &Error{Code: CodeJSONPartMissing}
	}
	return request, binaries, nil
}

func resolveReferences(operation, smContextRef string, request json.RawMessage, binaries []rawPart) (event.Input, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(request, &fields); err != nil {
		return event.Input{}, &Error{Code: CodeJSONInvalid, Err: err}
	}
	byContentID := map[string]rawPart{}
	for _, part := range binaries {
		byContentID[part.contentID] = part
	}

	referenced := map[string]string{}
	input := event.Input{Operation: operation, SmContextRef: smContextRef, Request: request}

	ref, err := contentIDReference(fields, "n1SmMsg")
	if err != nil {
		return event.Input{}, err
	}
	if ref != nil {
		part, err := bindPart(byContentID, referenced, *ref, slot.MediaTypeNAS, "n1SmMsg")
		if err != nil {
			return event.Input{}, err
		}
		input.N1 = &event.BinaryPart{ContentID: part.contentID, MediaType: part.mediaType, Bytes: part.bytes}
	}

	ref, err = contentIDReference(fields, "n2SmInfo")
	if err != nil {
		return event.Input{}, err
	}
	var n2Bound bool
	if ref != nil {
		part, err := bindPart(byContentID, referenced, *ref, slot.MediaTypeNGAP, "n2SmInfo")
		if err != nil {
			return event.Input{}, err
		}
		input.N2 = &event.BinaryPart{ContentID: part.contentID, MediaType: part.mediaType, Bytes: part.bytes}
		n2Bound = true
	}

	ref, err = contentIDReference(fields, "n2SmInfoExt1")
	if err != nil {
		return event.Input{}, err
	}
	var ext1Bound bool
	if ref != nil {
		part, err := bindPart(byContentID, referenced, *ref, slot.MediaTypeNGAP, "n2SmInfoExt1")
		if err != nil {
			return event.Input{}, err
		}
		input.N2Ext1 = &event.BinaryPart{ContentID: part.contentID, MediaType: part.mediaType, Bytes: part.bytes}
		ext1Bound = true
	}

	input.N2Type, err = stringField(fields, "n2SmInfoType")
	if err != nil {
		return event.Input{}, err
	}
	input.N2Ext1Type, err = stringField(fields, "n2SmInfoTypeExt1")
	if err != nil {
		return event.Input{}, err
	}
	if input.N2Type != "" && !n2Bound {
		return event.Input{}, &Error{Code: CodeN2TypeWithoutPart, Detail: input.N2Type}
	}
	if input.N2Ext1Type != "" && !ext1Bound {
		return event.Input{}, &Error{Code: CodeN2Ext1TypeWithoutPart, Detail: input.N2Ext1Type}
	}
	for _, part := range binaries {
		if _, used := referenced[part.contentID]; !used {
			return event.Input{}, &Error{Code: CodeContentIDUnreferenced, Detail: part.contentID}
		}
	}
	return input, nil
}

func bindPart(byContentID map[string]rawPart, referenced map[string]string, contentID, expectedMediaType, field string) (rawPart, error) {
	part, ok := byContentID[contentID]
	if !ok {
		return rawPart{}, &Error{Code: CodeContentIDUnresolved, Detail: contentID}
	}
	if existing, duplicate := referenced[contentID]; duplicate {
		return rawPart{}, &Error{Code: CodeContentIDDuplicateRef, Detail: existing + "/" + field}
	}
	if part.mediaType != expectedMediaType {
		return rawPart{}, &Error{Code: CodeMediaTypeMismatch, Detail: field + "=" + part.mediaType}
	}
	referenced[contentID] = field
	return part, nil
}

func contentIDReference(fields map[string]json.RawMessage, name string) (*string, error) {
	raw, ok := fields[name]
	if !ok {
		return nil, nil
	}
	var reference struct {
		ContentID string `json:"contentId"`
	}
	if err := json.Unmarshal(raw, &reference); err != nil {
		return nil, &Error{Code: CodeJSONInvalid, Detail: name, Err: err}
	}
	contentID := normalizeContentID(reference.ContentID)
	if contentID == "" {
		return nil, &Error{Code: CodeReferenceContentIDMissing, Detail: name}
	}
	return &contentID, nil
}

func stringField(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", &Error{Code: CodeJSONInvalid, Detail: name, Err: err}
	}
	return value, nil
}

func normalizeContentID(value string) string {
	value = strings.TrimSpace(value)
	for strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") && len(value) >= 2 {
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	return value
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = MaxPartBytes
	}
	payload, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			return nil, &Error{Code: CodeBodyTooLarge, Detail: maxBytes.Error(), Err: err}
		}
		return nil, &Error{Code: CodeJSONInvalid, Detail: "body read failed", Err: err}
	}
	if int64(len(payload)) > limit {
		return nil, &Error{Code: CodeBodyTooLarge}
	}
	return payload, nil
}
