// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dpop

import (
	"testing"
	"time"
)

// The public ES256 proof and example access token are RFC 9449 Figure 13:
// https://www.rfc-editor.org/rfc/rfc9449.html#section-7.1
// Keeping the external vector ensures our signer and verifier cannot agree on
// the same incompatible serialization, signature encoding or token hash.
func TestRFC9449ProtectedResourceProof(t *testing.T) {
	const proof = "eyJ0eXAiOiJkcG9wK2p3dCIsImFsZyI6IkVTMjU2IiwiandrIjp7Imt0eSI6IkVDIiwieCI6Imw4dEZyaHgtMzR0VjNoUklDUkRZOXpDa0RscEJoRjQyVVFVZldWQVdCRnMiLCJ5IjoiOVZFNGpmX09rX282NHpiVFRsY3VOSmFqSG10NnY5VERWclUwQ2R2R1JEQSIsImNydiI6IlAtMjU2In19." +
		"eyJqdGkiOiJlMWozVl9iS2ljOC1MQUVCIiwiaHRtIjoiR0VUIiwiaHR1IjoiaHR0cHM6Ly9yZXNvdXJjZS5leGFtcGxlLm9yZy9wcm90ZWN0ZWRyZXNvdXJjZSIsImlhdCI6MTU2MjI2MjYxOCwiYXRoIjoiZlVIeU8ycjJaM0RaNTNFc05yV0JiMHhXWG9hTnk1OUlpS0NBcWtzbVFFbyJ9." +
		"2oW9RP35yRqzhrtNP86L-Ey71EOptxRimPPToA1plemAgR6pxHF8y6-yqyVnmcw6Fy1dqd-jfxSYoMxhAJpLjA"
	const token = "Kz~8mXK1EalYznwH-LC-1fBAo.4Ljp~zsPE_NeO.gxU"
	issued := time.Unix(1562262618, 0)
	claims, err := Verify(proof, Expected{
		Method: "GET", URL: "https://resource.example.org/protectedresource",
		Token: token, Now: issued, Window: time.Minute,
	})
	if err != nil {
		t.Fatal("rejected the RFC 9449 ES256 proof:", err)
	}
	if claims.ID != "e1j3V_bKic8-LAEB" || !claims.IssuedAt.Equal(issued) || claims.Nonce != "" {
		t.Fatal("reference proof claims changed")
	}
}
