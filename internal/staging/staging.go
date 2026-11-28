// Package staging implements the Host-owned local staging contract from F05
// §7–§9. The Plugin writes inbound raw N1/N2 bytes under the Host-supplied
// inbound root and reads only the exact outbound files named by the current
// delivery.dispatch. It never owns a staging root, ArtifactStore or artifactRef,
// and it never applies Base64 or an extra digest.
package staging

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/odango-chan/ca-openamsm-plugin/internal/slot"
)

// Error carries a stable diagnostic code for tests and logs.
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return e.Code
	}
	return e.Code + ": " + e.Detail
}

const (
	CodeRootMissing        = "STAGING_ROOT_MISSING"
	CodeRootNotDirectory   = "STAGING_ROOT_NOT_DIRECTORY"
	CodeEventIDInvalid     = "STAGING_EVENT_ID_INVALID"
	CodeSlotUnknown        = "STAGING_SLOT_UNKNOWN"
	CodeMediaTypeMismatch  = "STAGING_MEDIA_TYPE_MISMATCH"
	CodePathEmpty          = "STAGING_PATH_EMPTY"
	CodePathAbsolute       = "STAGING_PATH_ABSOLUTE"
	CodePathEscape         = "STAGING_PATH_ESCAPE"
	CodePathSymlink        = "STAGING_PATH_SYMLINK"
	CodeFileMissing        = "STAGING_FILE_MISSING"
	CodeFileEmpty          = "STAGING_FILE_EMPTY"
	CodeByteLengthMismatch = "STAGING_BYTE_LENGTH_MISMATCH"
	CodeWriteFailed        = "STAGING_WRITE_FAILED"
)

var eventIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Descriptor is one ingress.event artifact descriptor. Bytes is internal only
// and never appears on the wire.
type Descriptor struct {
	Slot        string `json:"slot"`
	MediaType   string `json:"mediaType"`
	ContentID   string `json:"contentId,omitempty"`
	StagingPath string `json:"stagingPath"`
	ByteLength  int64  `json:"byteLength"`
}

// Store owns the two Host-supplied staging roots for one Plugin process.
type Store struct {
	inbound      string
	outbound     string
	inboundReal  string
	outboundReal string
}

// New validates both Host-supplied roots. The Plugin must not create them.
func New(inboundDir, outboundDir string) (*Store, error) {
	inbound, inboundReal, err := validateRoot(inboundDir)
	if err != nil {
		return nil, err
	}
	outbound, outboundReal, err := validateRoot(outboundDir)
	if err != nil {
		return nil, err
	}
	return &Store{inbound: inbound, outbound: outbound, inboundReal: inboundReal, outboundReal: outboundReal}, nil
}

func validateRoot(value string) (string, string, error) {
	if strings.TrimSpace(value) == "" {
		return "", "", &Error{Code: CodeRootMissing, Detail: "empty directory"}
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", "", &Error{Code: CodeRootMissing, Detail: err.Error()}
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", "", &Error{Code: CodeRootMissing, Detail: value}
	}
	if !info.IsDir() {
		return "", "", &Error{Code: CodeRootNotDirectory, Detail: value}
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", "", &Error{Code: CodeRootMissing, Detail: value}
	}
	return absolute, real, nil
}

// WriteInbound writes one raw part under a per-Event directory and returns its
// relative descriptor. File names come from the frozen slot vocabulary only.
func (s *Store) WriteInbound(eventID, slotName string, contentID string, mediaType string, payload []byte) (Descriptor, error) {
	if !eventIDPattern.MatchString(eventID) {
		return Descriptor{}, &Error{Code: CodeEventIDInvalid, Detail: eventID}
	}
	expected, ok := slot.MediaType(slotName)
	if !ok || !slot.IsInbound(slotName) {
		return Descriptor{}, &Error{Code: CodeSlotUnknown, Detail: slotName}
	}
	if mediaType != expected {
		return Descriptor{}, &Error{Code: CodeMediaTypeMismatch, Detail: slotName + "/" + mediaType}
	}
	if len(payload) == 0 {
		return Descriptor{}, &Error{Code: CodeFileEmpty, Detail: slotName}
	}
	fileName, _ := slot.FileName(slotName)
	directory := filepath.Join(s.inbound, "event-"+eventID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Descriptor{}, &Error{Code: CodeWriteFailed, Detail: err.Error()}
	}
	target := filepath.Join(directory, fileName)
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Descriptor{}, &Error{Code: CodeWriteFailed, Detail: err.Error()}
	}
	if _, err := file.Write(payload); err != nil {
		file.Close()
		return Descriptor{}, &Error{Code: CodeWriteFailed, Detail: err.Error()}
	}
	if err := file.Close(); err != nil {
		return Descriptor{}, &Error{Code: CodeWriteFailed, Detail: err.Error()}
	}
	return Descriptor{
		Slot:        slotName,
		MediaType:   mediaType,
		ContentID:   contentID,
		StagingPath: path.Join("event-"+eventID, fileName),
		ByteLength:  int64(len(payload)),
	}, nil
}

// RemoveEvent removes the per-Event inbound directory after a failed emit. The
// Plugin never cleans files after a successful emit: Host imports and cleans.
func (s *Store) RemoveEvent(eventID string) error {
	if !eventIDPattern.MatchString(eventID) {
		return &Error{Code: CodeEventIDInvalid, Detail: eventID}
	}
	return os.RemoveAll(filepath.Join(s.inbound, "event-"+eventID))
}

// ReadOutbound resolves one delivery output descriptor and returns its exact
// bytes after containment and byteLength verification.
func (s *Store) ReadOutbound(descriptor Descriptor) ([]byte, error) {
	expected, ok := slot.MediaType(descriptor.Slot)
	if !ok || !slot.IsOutbound(descriptor.Slot) {
		return nil, &Error{Code: CodeSlotUnknown, Detail: descriptor.Slot}
	}
	if descriptor.MediaType != expected {
		return nil, &Error{Code: CodeMediaTypeMismatch, Detail: descriptor.Slot + "/" + descriptor.MediaType}
	}
	target, err := ResolveWithin(s.outbound, descriptor.StagingPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &Error{Code: CodeFileMissing, Detail: descriptor.StagingPath}
		}
		return nil, &Error{Code: CodeFileMissing, Detail: err.Error()}
	}
	if !info.Mode().IsRegular() {
		return nil, &Error{Code: CodePathSymlink, Detail: descriptor.StagingPath}
	}
	real, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, &Error{Code: CodeFileMissing, Detail: descriptor.StagingPath}
	}
	if !within(s.outboundReal, real) {
		return nil, &Error{Code: CodePathEscape, Detail: descriptor.StagingPath}
	}
	payload, err := os.ReadFile(target)
	if err != nil {
		return nil, &Error{Code: CodeFileMissing, Detail: descriptor.StagingPath}
	}
	if len(payload) == 0 {
		return nil, &Error{Code: CodeFileEmpty, Detail: descriptor.Slot}
	}
	if int64(len(payload)) != descriptor.ByteLength {
		return nil, &Error{Code: CodeByteLengthMismatch, Detail: fmt.Sprintf("declared=%d actual=%d", descriptor.ByteLength, len(payload))}
	}
	return payload, nil
}

// ResolveWithin validates a relative staging path and returns its absolute
// path inside root. Absolute paths, volume names, traversal, symlink targets and
// Windows alternate data streams are rejected.
func ResolveWithin(root, relative string) (string, error) {
	if strings.TrimSpace(relative) == "" {
		return "", &Error{Code: CodePathEmpty}
	}
	normalized := strings.ReplaceAll(relative, "\\", "/")
	if strings.Contains(normalized, "\x00") || strings.Contains(normalized, ":") {
		return "", &Error{Code: CodePathAbsolute, Detail: relative}
	}
	if path.IsAbs(normalized) {
		return "", &Error{Code: CodePathAbsolute, Detail: relative}
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", &Error{Code: CodePathEscape, Detail: relative}
	}
	rootAbsolute, err := filepath.Abs(root)
	if err != nil {
		return "", &Error{Code: CodeRootMissing, Detail: err.Error()}
	}
	target := filepath.Join(rootAbsolute, filepath.FromSlash(cleaned))
	targetAbsolute, err := filepath.Abs(target)
	if err != nil {
		return "", &Error{Code: CodePathEscape, Detail: relative}
	}
	if !within(rootAbsolute, targetAbsolute) {
		return "", &Error{Code: CodePathEscape, Detail: relative}
	}
	if info, err := os.Lstat(targetAbsolute); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", &Error{Code: CodePathSymlink, Detail: relative}
	}
	return targetAbsolute, nil
}

func within(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if relative == "." {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
