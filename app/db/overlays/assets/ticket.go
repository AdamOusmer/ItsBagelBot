// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package assets runs the upload pipeline of the overlays service: an RPC hands out a short-lived ticket,
// then the dashboard streams the bytes to the internal HTTP listener.
package assets

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const TicketTTL = 10 * time.Minute

var ErrBadTicket = errors.New("invalid_ticket")

type Purpose string

const (
	PurposeAsset  Purpose = "asset"
	PurposeImport Purpose = "import"
)

type Ticket struct {
	UserID  uint64  `json:"u"`
	Purpose Purpose `json:"p"`
	MaxSize int64   `json:"m"`
	Expires int64   `json:"e"`
	DryRun  bool    `json:"d,omitempty"`
	Name    string  `json:"n,omitempty"`
}

type Signer struct {
	Secret []byte
}

func (s Signer) mac(payload string) []byte {
	sum := hmac.New(sha256.New, s.Secret)
	sum.Write([]byte(payload))
	return sum.Sum(nil)
}

func (s Signer) Issue(t Ticket) (string, error) {
	body, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	return payload + "." + base64.RawURLEncoding.EncodeToString(s.mac(payload)), nil
}

func (s Signer) Verify(token string, now time.Time) (Ticket, error) {
	payload, signature, ok := strings.Cut(token, ".")
	if !ok {
		return Ticket{}, ErrBadTicket
	}
	got, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(got, s.mac(payload)) {
		return Ticket{}, ErrBadTicket
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Ticket{}, ErrBadTicket
	}
	var t Ticket
	if json.Unmarshal(body, &t) != nil || now.Unix() >= t.Expires {
		return Ticket{}, ErrBadTicket
	}
	return t, nil
}
