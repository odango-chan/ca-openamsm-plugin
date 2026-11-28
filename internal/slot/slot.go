// Package slot owns the frozen OpenAMSM artifact slot vocabulary shared by the
// Plugin manifest, Event normalization, staging and Delivery.
//
// The vocabulary follows ca-agent-next F05 v0.4.10 §10 plus the n2Ext1.request
// completion required by F06 v0.3.7 §4.1 (P2-01). Media types are fixed by the
// same design.
package slot

const (
	MediaTypeNAS  = "application/vnd.3gpp.5gnas"
	MediaTypeNGAP = "application/vnd.3gpp.ngap"

	N1Request      = "n1.request"
	N2Request      = "n2.request"
	N2Ext1Request  = "n2Ext1.request"
	N1Confirmation = "n1.confirmation"
	N2Confirmation = "n2.confirmation"
	N1Response     = "n1.response"
	N2Response     = "n2.response"
)

var mediaTypes = map[string]string{
	N1Request:      MediaTypeNAS,
	N2Request:      MediaTypeNGAP,
	N2Ext1Request:  MediaTypeNGAP,
	N1Confirmation: MediaTypeNAS,
	N2Confirmation: MediaTypeNGAP,
	N1Response:     MediaTypeNAS,
	N2Response:     MediaTypeNGAP,
}

var fileNames = map[string]string{
	N1Request:      "n1.bin",
	N2Request:      "n2.bin",
	N2Ext1Request:  "n2ext1.bin",
	N1Confirmation: "n1confirmation.bin",
	N2Confirmation: "n2confirmation.bin",
	N1Response:     "n1response.bin",
	N2Response:     "n2response.bin",
}

var inbound = map[string]struct{}{
	N1Request: {}, N2Request: {}, N2Ext1Request: {}, N1Confirmation: {}, N2Confirmation: {},
}

var outbound = map[string]struct{}{N1Response: {}, N2Response: {}}

// MediaType returns the declared media type for a known slot.
func MediaType(name string) (string, bool) {
	value, ok := mediaTypes[name]
	return value, ok
}

// IsKnown reports whether the slot belongs to the frozen vocabulary.
func IsKnown(name string) bool {
	_, ok := mediaTypes[name]
	return ok
}

// IsInbound reports whether the slot is an Event input slot.
func IsInbound(name string) bool {
	_, ok := inbound[name]
	return ok
}

// IsOutbound reports whether the slot is a Delivery output slot.
func IsOutbound(name string) bool {
	_, ok := outbound[name]
	return ok
}

// FileName returns the deterministic staging file name for a slot. The Plugin
// never derives a file name from peer input.
func FileName(name string) (string, bool) {
	value, ok := fileNames[name]
	return value, ok
}
