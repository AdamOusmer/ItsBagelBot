// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bridge

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/pkg/dpop"
	"github.com/gofiber/fiber/v3"
)

// This bounded synthetic guard is only for the opt-in Tunnel fixture. The real
// Users authority owns encrypted nonces, durable replay receipts and revocation.
type probeProofGuard struct {
	host, token, jkt string
	mu               sync.Mutex
	nonces           [3]string
	issued           [3]time.Time
	seen             map[string]bool
}

func newProbeProofGuard(host, token, jkt string) *probeProofGuard {
	return &probeProofGuard{host: host, token: token, jkt: jkt, seen: make(map[string]bool)}
}
func (g *probeProofGuard) authorize(c fiber.Ctx) error {
	if subtle.ConstantTimeCompare([]byte(c.Get("Authorization")), []byte("DPoP "+g.token)) != 1 {
		return c.Status(401).Send(nil)
	}
	claims, err := dpop.Verify(c.Get("DPoP"), dpop.Expected{Method: c.Method(), URL: "https://" + g.host + c.Path(), Token: g.token, Now: time.Now(), Window: time.Minute})
	if err != nil || claims.Thumbprint != g.jkt {
		return c.Status(401).Send(nil)
	}
	g.mu.Lock()
	slot := proofProbeSlot(c.Path())
	if !g.validNonce(slot, claims.Nonce) {
		err := g.challenge(c, slot)
		g.mu.Unlock()
		return err
	}
	if g.seen[claims.ID] || len(g.seen) >= 64 {
		g.mu.Unlock()
		return c.Status(403).Send(nil)
	}
	g.seen[claims.ID] = true
	g.mu.Unlock()
	return c.Next()
}
func proofProbeSlot(path string) int {
	switch path {
	case protocol.BindPath:
		return 1
	case protocol.MigrationPath:
		return 2
	default:
		return 0
	}
}
func (g *probeProofGuard) validNonce(slot int, nonce string) bool {
	return nonce != "" && nonce == g.nonces[slot] && time.Since(g.issued[slot]) < 2*time.Minute
}
func (g *probeProofGuard) challenge(c fiber.Ctx, slot int) error {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return c.SendStatus(503)
	}
	g.nonces[slot] = base64.RawURLEncoding.EncodeToString(raw[:])
	g.issued[slot] = time.Now()
	c.Set("DPoP-Nonce", g.nonces[slot])
	c.Set("WWW-Authenticate", `DPoP error="use_dpop_nonce"`)
	return c.Status(http.StatusUnauthorized).Send(nil)
}
func tunnelProofKey(t *testing.T) *dpop.Key {
	t.Helper()
	path := os.Getenv("BAGEL_TUNNEL_PROOF_KEY_FILE")
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("probe proof key requires owner-only permissions")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("probe proof key unavailable")
	}
	key, err := dpop.ParseKey(raw)
	if err != nil {
		t.Fatal("probe proof key rejected")
	}
	return key
}
