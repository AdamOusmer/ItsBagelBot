// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordrate

import (
	"net/http"

	"ItsBagelBot/internal/discordapi"
)

// NewClient builds the Discord REST client both Discord services hold: the
// stock internal/discordapi client with the shared-bucket gate installed on
// its transport, so every request it makes pays one token on the way out.
//
// This replaced a LimitedClient decorator that redeclared all 34 REST methods
// the services used -- a `rest` interface plus one forwarder each -- to pay
// the gate. Two things made the method set the wrong seam. A method added to
// discordapi and used without a forwarder shipped ungated and compiled fine
// (only a reflection sweep in the tests stood between that and production),
// and the wrapper charged a token for calls that never reach the wire:
// ModifyGuild with an empty patch sends no request, and now costs nothing.
// A RoundTripper cannot be bypassed, because there is no path from the client
// to Discord that does not go through it.
//
// The gate is GLOBAL -- one Valkey key for the whole bot token, see globalKey
// -- so the transport needs nothing out of the request to pick a bucket. Had
// the gate been per-route, this seam would have had to rebuild Discord's route
// key (method + path pattern + major parameter) from the request path, which
// is the one thing the 34 method wrappers got for free.
func NewClient(botToken string, gate Gate) *discordapi.Client {
	client := discordapi.NewClient(botToken)
	client.SetTransport(gatedTransport{gate: gate, next: http.DefaultTransport})
	return client
}

// gatedTransport is the Decorator around the REST client, moved off its method
// set and onto the wire.
type gatedTransport struct {
	gate Gate
	next http.RoundTripper
}

// RoundTrip pays one token, then sends. A refused token returns ErrRateLimited
// without opening a connection; http.Client wraps that in *url.Error, which
// errors.Is unwraps, so callers still match the sentinel.
func (t gatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.gate.Take(req.Context()); err != nil {
		return nil, err
	}
	return t.next.RoundTrip(req)
}
