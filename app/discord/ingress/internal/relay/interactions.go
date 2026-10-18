// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package relay

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

const (
	maxInteractionBody = 1 << 20
	publishTimeout     = 2 * time.Second
	interactionPing    = 1
	pongBody           = `{"type":1}`
	deferBody          = `{"type":5}`
)

var errBadVerifyKey = errors.New("discord verify key is not an ed25519 public key")

type VerifyKeyFunc func(ctx context.Context) (string, error)

type Interactions struct {
	relay *Relay
	fetch VerifyKeyFunc

	mu  sync.Mutex
	key ed25519.PublicKey
}

func NewInteractions(r *Relay, fetch VerifyKeyFunc) *Interactions {
	return &Interactions{relay: r, fetch: fetch}
}

func (i *Interactions) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	key, err := i.verifyKey(req.Context())
	if err != nil {
		i.relay.log().Warn("interaction verify key unavailable", zap.Error(err))
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxInteractionBody))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !validSignature(key, req.Header, body) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var head struct {
		Type int    `json:"type"`
		ID   string `json:"id"`
	}
	if err := codec.Unmarshal(body, &head); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if head.Type == interactionPing {
		respondJSON(w, pongBody)
		return
	}
	i.publishAndAck(w, req, body, head.ID)
}

func (i *Interactions) publishAndAck(w http.ResponseWriter, req *http.Request, body []byte, id string) {
	ctx, cancel := context.WithTimeout(req.Context(), publishTimeout)
	defer cancel()
	if err := i.relay.PublishInteraction(ctx, body, id); err != nil {
		i.relay.log().Warn("interaction publish failed", zap.String("interaction_id", id), zap.Error(err))
		http.Error(w, "publish failed", http.StatusInternalServerError)
		return
	}
	respondJSON(w, deferBody)
}

func (i *Interactions) verifyKey(ctx context.Context) (ed25519.PublicKey, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.key != nil {
		return i.key, nil
	}
	encoded, err := i.fetch(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, errBadVerifyKey
	}
	i.key = raw
	return i.key, nil
}

func validSignature(key ed25519.PublicKey, h http.Header, body []byte) bool {
	sig, err := hex.DecodeString(h.Get("X-Signature-Ed25519"))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	msg := append([]byte(h.Get("X-Signature-Timestamp")), body...)
	return ed25519.Verify(key, msg, sig)
}

func respondJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}
