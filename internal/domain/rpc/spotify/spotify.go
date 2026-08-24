// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package spotifyrpc holds the shared wire types for the Spotify refresh-token
// custody RPC the modules service owns. A broadcaster's refresh token is an
// account secret: like the Govee API key it is stored encrypted at rest (Tink
// AEAD, the modules service's own keyset) and never leaves the fleet except
// decrypted to the gateway, the one service that exchanges it for access
// tokens against accounts.spotify.com.
//
// Two subject families, split by trust (the goveerpc pattern):
//
//   - Dashboard verbs under a public-ish prefix (default
//     "bagel.rpc.modules.spotify"): "set" stores a token, "clear" removes it,
//     "status" reports only whether one is on file. None ever echoes the
//     token back — the console shows "connected", never the value.
//   - One internal verb, "bagel.rpc.internal.spotify.key.get",
//     export/import-scoped at the NATS account level to the gateway alone,
//     mirroring the users service's token/email RPCs. It returns the
//     decrypted refresh token so the gateway can mint a short-lived access
//     token.
package spotifyrpc

// RefreshTokenSetRequest stores (or replaces) a broadcaster's connected
// Spotify account credential: the OAuth refresh token minted by the console's
// connect flow.
type RefreshTokenSetRequest struct {
	UserID       string `json:"user_id"`
	RefreshToken string `json:"refresh_token"`
}

// RefreshTokenClearRequest removes a broadcaster's stored refresh token
// ("disconnect Spotify").
type RefreshTokenClearRequest struct {
	UserID string `json:"user_id"`
}

// RefreshTokenStatusRequest asks whether a broadcaster has a token on file.
type RefreshTokenStatusRequest struct {
	UserID string `json:"user_id"`
}

// RefreshTokenStatusReply reports only presence, never the token itself.
type RefreshTokenStatusReply struct {
	Present bool   `json:"present"`
	Error   string `json:"error,omitempty"`
}

// RefreshTokenMutateReply is the ack for set/clear: a bare error envelope.
type RefreshTokenMutateReply struct {
	Error string `json:"error,omitempty"`
}

// RefreshTokenGetRequest is the internal decrypt request the gateway makes.
type RefreshTokenGetRequest struct {
	UserID string `json:"user_id"`
}

// RefreshTokenGetReply carries the decrypted refresh token or a terminal
// error. An empty RefreshToken with empty Error means the broadcaster has not
// connected Spotify yet (the caller treats that as "not set up", not a
// failure).
type RefreshTokenGetReply struct {
	RefreshToken string `json:"refresh_token,omitempty"`
	Error        string `json:"error,omitempty"`
}
