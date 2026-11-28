// Package event implements the frozen F06 §4.1 normalized Event contract:
// three Event types, their payload projection, minimal T0 NAS classification and
// Content-ID/slot mapping.
package event

import "errors"

// N1Class is the minimal 5GSM discriminator result.
type N1Class string

const (
	N1Modify          N1Class = "0xC9"
	N1Release         N1Class = "0xD1"
	N1ReleaseComplete N1Class = "0xD4"
)

// Classifier is the narrow T0 5GSM discriminator seam.
//
// Production wiring must use the exact package/API/version published by
// ca-client-tools#44. That API is not published yet, so the production adapter
// (BlockedClassifier) fails closed and AC-009-04 production binding stays
// BLOCKED_EXTERNAL_INPUT. TestDoubleClassifier exists only so tests can verify
// Event mapping; it must never be reported as a production classifier PASS.
type Classifier interface {
	ClassifyN1(payload []byte) (N1Class, error)
}

var (
	// ErrBlockedExternalInput is returned by the production adapter while the
	// shared T0 classifier API is unpublished.
	ErrBlockedExternalInput = errors.New("T0 5GSM classifier API not published (ca-client-tools#44): BLOCKED_EXTERNAL_INPUT")
	// ErrMalformedN1 covers a truncated header or a wrong EPD.
	ErrMalformedN1 = errors.New("malformed 5GSM header")
	// ErrUnsupportedN1 covers a well-formed header with an unsupported message type.
	ErrUnsupportedN1 = errors.New("unsupported 5GSM message type")
)

// BlockedClassifier is the production adapter. It never parses NAS.
type BlockedClassifier struct{}

// ClassifyN1 always reports the external block.
func (BlockedClassifier) ClassifyN1([]byte) (N1Class, error) {
	return "", ErrBlockedExternalInput
}

// TestDoubleClassifier implements only the F06 minimum header contract for
// tests: at least 4 bytes, payload[0] == 0x2e, message type at offset 3.
//
// It is not a NAS parser and is not a substitute for the shared ca-client-tools
// API. The plugin binary enables it only through the explicit
// --test-only-classifier-double test flag.
type TestDoubleClassifier struct{}

// ClassifyN1 applies the minimum header check.
func (TestDoubleClassifier) ClassifyN1(payload []byte) (N1Class, error) {
	if len(payload) < 4 || payload[0] != 0x2e {
		return "", ErrMalformedN1
	}
	switch payload[3] {
	case 0xc9:
		return N1Modify, nil
	case 0xd1:
		return N1Release, nil
	case 0xd4:
		return N1ReleaseComplete, nil
	default:
		return "", ErrUnsupportedN1
	}
}
