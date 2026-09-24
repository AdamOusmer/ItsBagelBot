// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func verifyTebexSignature(body []byte, signature, secret string) bool {
	provided, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return false
	}
	return hmac.Equal(provided, tebexSignature(body, secret))
}

func tebexSignature(body []byte, secret string) []byte {
	bodyHash := sha256.Sum256(body)
	bodyHashHex := make([]byte, hex.EncodedLen(len(bodyHash)))
	hex.Encode(bodyHashHex, bodyHash[:])

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(bodyHashHex)
	return mac.Sum(nil)
}
