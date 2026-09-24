// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordrate

import (
	"net/http"

	"ItsBagelBot/internal/discordapi"
)

func NewClient(botToken string, gate Gate) *discordapi.Client {
	client := discordapi.NewClient(botToken)
	client.SetTransport(gatedTransport{gate: gate, next: http.DefaultTransport})
	return client
}

type gatedTransport struct {
	gate Gate
	next http.RoundTripper
}

func (t gatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.gate.Take(req.Context()); err != nil {
		return nil, err
	}
	return t.next.RoundTrip(req)
}
