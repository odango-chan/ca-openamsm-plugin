// Package testsupport provides shared SBI/Event fixtures for module tests. It is
// imported only by _test files.
package testsupport

import (
	"bytes"
	"mime/multipart"
	"net/textproto"
)

const (
	MediaNAS  = "application/vnd.3gpp.5gnas"
	MediaNGAP = "application/vnd.3gpp.ngap"
)

// BinaryPart is one multipart binary fixture part.
type BinaryPart struct {
	ContentID string
	MediaType string
	Bytes     []byte
}

// N1Message builds the F06 minimum 5GSM header: EPD 0x2e, PSI, PTI, then the
// message type at offset 3.
func N1Message(messageType byte, tail ...byte) []byte {
	payload := make([]byte, 0, 4+len(tail))
	payload = append(payload, 0x2e, 0x05, 0x01, messageType)
	payload = append(payload, tail...)
	return payload
}

var (
	// N1Modify is a PDU Session Modification Request fixture (0xC9).
	N1Modify = N1Message(0xc9, 0x28, 0x11, 0x22, 0x33)
	// N1Release is a PDU Session Release Request fixture (0xD1).
	N1Release = N1Message(0xd1, 0x41, 0x01)
	// N1ReleaseComplete is a PDU Session Release Complete fixture (0xD4).
	N1ReleaseComplete = N1Message(0xd4, 0x00)
	// N1OwnerResponse is an outbound N1 fixture.
	N1OwnerResponse = N1Message(0xca, 0x77, 0x66)
	// N2SetupResponse is an NGAP PDU Session Resource Setup Response fixture.
	N2SetupResponse = []byte{0x00, 0x0f, 0x40, 0x01, 0x02, 0x03, 0x04}
	// N2ModResponse is an NGAP PDU Session Resource Modify Response fixture.
	N2ModResponse = []byte{0x00, 0x0f, 0x40, 0x02, 0x03, 0x04, 0x05}
	// N2RelResponse is an NGAP PDU Session Resource Release Response fixture.
	N2RelResponse = []byte{0x00, 0x0f, 0x40, 0x03, 0x04, 0x05, 0x06}
	// N2Ext1Response is a second NGAP container fixture for n2SmInfoExt1.
	N2Ext1Response = []byte{0x00, 0x0f, 0x40, 0x0a, 0x0b}
	// N2OwnerResponse is an outbound NGAP fixture.
	N2OwnerResponse = []byte{0x00, 0x0f, 0x40, 0x7f, 0x01}
)

// Multipart builds a multipart/related body with one JSON part followed by the
// supplied binary parts. Content-IDs are wrapped in angle brackets.
func Multipart(jsonPart []byte, parts ...BinaryPart) (string, []byte) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", "application/json")
	jsonWriter, _ := writer.CreatePart(header)
	_, _ = jsonWriter.Write(jsonPart)
	for _, binary := range parts {
		partHeader := textproto.MIMEHeader{}
		partHeader.Set("Content-Type", binary.MediaType)
		if binary.ContentID != "" {
			partHeader.Set("Content-ID", "<"+binary.ContentID+">")
		}
		writerPart, _ := writer.CreatePart(partHeader)
		_, _ = writerPart.Write(binary.Bytes)
	}
	_ = writer.Close()
	return "multipart/related; boundary=" + writer.Boundary(), buffer.Bytes()
}

// CreateJSON is a verbatim-shaped SmContextCreateData fixture. It is never
// normalized or rewritten by the Plugin.
func CreateJSON() []byte {
	return []byte(`{"supi":"imsi-001010000000001","pduSessionId":5,"dnn":"internet","requestType":"INITIAL_REQUEST","servingNfId":"4947a4e1-1111-2222-3333-444455556666","n1SmMsg":{"contentId":"n1SmMsg"},"n2SmInfo":{"contentId":"n2SmInfo"},"n2SmInfoType":"PDU_RES_SETUP_REQ"}`)
}

// CreateJSONN1Only is a Create fixture without an N2 SM information part.
func CreateJSONN1Only() []byte {
	return []byte(`{"supi":"imsi-001010000000001","pduSessionId":5,"dnn":"internet","requestType":"INITIAL_REQUEST","n1SmMsg":{"contentId":"n1SmMsg"}}`)
}

// UpdateJSONModify is a SmContextUpdateData fixture carrying N1 and optional N2.
func UpdateJSONModify() []byte {
	return []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"},"n2SmInfo":{"contentId":"n2SmInfo"},"n2SmInfoType":"PDU_RES_MOD_REQ"}`)
}

// UpdateContinuationJSON carries N1 Release Complete only.
func UpdateContinuationJSON() []byte {
	return []byte(`{"n1SmMsg":{"contentId":"n1SmMsg"}}`)
}

// UpdateN2ContinuationJSON carries only an N2 completion response.
func UpdateN2ContinuationJSON(n2Type string) []byte {
	return []byte(`{"n2SmInfo":{"contentId":"n2SmInfo"},"n2SmInfoType":"` + n2Type + `"}`)
}

// Response201Body is a Host-supplied Create response body fixture.
func Response201Body() []byte {
	return []byte(`{"smContextStatusUri":"https://smf.example.test/nsmf-pdusession/v1/sm-contexts/1/status"}`)
}
