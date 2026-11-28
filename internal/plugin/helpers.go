package plugin

import (
	"crypto/rand"
	"encoding/hex"
)

func randomSuffix() string {
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(buffer)
}
