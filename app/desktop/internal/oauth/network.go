// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package oauth

import "context"

// Network is supplied by the process owner. The credential-owning process uses
// an IPC implementation; it never constructs a direct HTTP client as fallback.
// The caller owns this port's lifetime, which may be shared with event traffic.
type Network interface {
	Do(context.Context, NetworkRequest) (NetworkResponse, error)
}

// NetworkRequest is a single native POST. It contains only the credentials and
// already-created proof needed by the configured service, never a private key.
type NetworkRequest struct {
	Endpoint    string
	Proof       string
	AccessToken string
	Form        string
	Revoke      bool
}

// NetworkResponse carries parsed, bounded fields from the network worker. Raw
// HTTP/JSON response bytes and headers never cross the network boundary.
type NetworkResponse struct {
	Status int
	Nonce  string
	Token  TokenReply
}

type TokenReply struct {
	Access  string `json:"access_token"`
	Type    string `json:"token_type"`
	Expires int64  `json:"expires_in"`
	Refresh string `json:"refresh_token"`
	Grant   string `json:"grant_id"`
	Device  string `json:"device_id"`
	Error   string `json:"error"`
}
