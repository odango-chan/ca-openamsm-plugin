package event

// n2SmInfoTypes is the closed N2SmInfoType enumeration from the fixed OpenAPI
// source:
//
//	env-openamsm@dc83ccbf5be54dcc7112dd43ba95ea6fa5975550
//	upstream/openamsm/lib/sbi/support/r19-20260402-openapitools-7.20.0/standard/
//	TS29502_Nsmf_PDUSession.yaml (TS 29.502 V19.5.0), components.schemas.N2SmInfoType
//
// TS 29.502 permits a forward-compatible string for future extensions. The
// SR-009 ingress profile intentionally fails closed for values outside this
// frozen enumeration (F06 §4.1 rule 3) instead of guessing their semantics.
// This list is source data, not an inferred mapping.
var n2SmInfoTypes = map[string]struct{}{
	"PDU_RES_SETUP_REQ":       {},
	"PDU_RES_SETUP_RSP":       {},
	"PDU_RES_SETUP_FAIL":      {},
	"PDU_RES_REL_CMD":         {},
	"PDU_RES_REL_RSP":         {},
	"PDU_RES_MOD_REQ":         {},
	"PDU_RES_MOD_RSP":         {},
	"PDU_RES_MOD_FAIL":        {},
	"PDU_RES_NTY":             {},
	"PDU_RES_NTY_REL":         {},
	"PDU_RES_MOD_IND":         {},
	"PDU_RES_MOD_CFM":         {},
	"PATH_SWITCH_REQ":         {},
	"PATH_SWITCH_SETUP_FAIL":  {},
	"PATH_SWITCH_REQ_ACK":     {},
	"PATH_SWITCH_REQ_FAIL":    {},
	"HANDOVER_REQUIRED":       {},
	"HANDOVER_CMD":            {},
	"HANDOVER_PREP_FAIL":      {},
	"HANDOVER_REQ_ACK":        {},
	"HANDOVER_RES_ALLOC_FAIL": {},
	"SECONDARY_RAT_USAGE":     {},
	"PDU_RES_MOD_IND_FAIL":    {},
	"UE_CONTEXT_RESUME_REQ":   {},
	"UE_CONTEXT_RESUME_RSP":   {},
	"UE_CONTEXT_SUSPEND_REQ":  {},
}

// continuationN2Types are the three N2 completion responses frozen by F06
// §4.1. They classify an UpdateSmContext ingress as continuation.
var continuationN2Types = map[string]struct{}{
	"PDU_RES_SETUP_RSP": {},
	"PDU_RES_MOD_RSP":   {},
	"PDU_RES_REL_RSP":   {},
}

// IsKnownN2Type reports whether the value belongs to the frozen enumeration.
func IsKnownN2Type(value string) bool {
	_, ok := n2SmInfoTypes[value]
	return ok
}

// IsContinuationN2Type reports whether the value is one of the three frozen N2
// completion responses.
func IsContinuationN2Type(value string) bool {
	_, ok := continuationN2Types[value]
	return ok
}

// N2Types returns the frozen enumeration for tests and documentation.
func N2Types() []string {
	values := make([]string, 0, len(n2SmInfoTypes))
	for value := range n2SmInfoTypes {
		values = append(values, value)
	}
	return values
}
