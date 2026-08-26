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
//     token back: the console shows "connected", never the value.
//   - Internal verbs under "bagel.rpc.internal.spotify.key",
//     export/import-scoped at the NATS account level to the gateway alone,
//     mirroring the users service's token/email RPCs. "get" returns the
//     decrypted refresh token so the gateway can mint a short-lived access
//     token; "rotate" writes a replacement back when Spotify rotates the
//     token on that exchange.
package spotifyrpc

// RefreshTokenSetRequest stores (or replaces) a broadcaster's connected
// Spotify account credential: the OAuth refresh token minted by the console's
// connect flow.
type RefreshTokenSetRequest struct {
	UserID       string `json:"user_id"`
	RefreshToken string `json:"refresh_token"`
	// Scopes is what Spotify granted with this token (see the gossip exchange
	// reply). Omitted on a write that is not a fresh grant, which leaves the
	// recorded set untouched.
	Scopes []string `json:"scopes,omitempty"`
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
	Present bool `json:"present"`
	// Scopes is what the stored grant covers. Empty with Present true means a
	// grant that predates scope recording: unknown, and to be treated as
	// missing whatever the caller needs.
	Scopes []string `json:"scopes,omitempty"`
	Error  string   `json:"error,omitempty"`
}

// RefreshTokenMutateReply is the ack for set/clear: a bare error envelope.
type RefreshTokenMutateReply struct {
	Error string `json:"error,omitempty"`
}

// RefreshTokenRotateRequest is the internal write-back the gateway makes when
// Spotify rotates a refresh token on exchange. Compare-and-swap: the store
// replaces the token only while PrevToken still matches what is on file, so a
// delayed write-back can never clobber a newer credential (a concurrent
// mint's rotation, or a fresh console reconnect). Both tokens ride the same
// account-scoped internal subject as the decrypt verb and are never logged.
type RefreshTokenRotateRequest struct {
	UserID    string `json:"user_id"`
	PrevToken string `json:"prev_token"`
	NewToken  string `json:"new_token"`
}

// RefreshTokenGetRequest is the internal decrypt request the gateway makes.
type RefreshTokenGetRequest struct {
	UserID string `json:"user_id"`
}

// RefreshTokenGetReply carries the broadcaster's whole decrypted Spotify
// credential set (their own application plus the grant minted against it) or
// a terminal error. Gossip needs all three on the same call (it authenticates
// the refresh exchange with the app that issued the grant), so they ride one
// reply rather than costing two round trips per chat command.
//
// Empty fields with an empty Error mean "not set up", which the caller treats
// as a state, not a failure. The two halves are set independently: an app with
// no RefreshToken is a broadcaster who pasted credentials but never finished
// the connect flow.
type RefreshTokenGetReply struct {
	RefreshToken string `json:"refresh_token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Error        string `json:"error,omitempty"`
}

// --- broadcaster-owned Spotify application ----------------------------------
//
// Every broadcaster registers their OWN Spotify application and pastes its
// client id and client secret into the console; the fleet no longer ships a
// global app. The client id is public by construction (it rides the authorize
// URL in the browser) so it is stored and echoed in the clear; the client
// secret is a third-party secret and gets the same sealed-at-rest treatment as
// the refresh token, under its own AAD label.
//
// The secret leaves the modules service on exactly one subject: the internal
// key.get above, imported by gossip alone: because gossip is the only service
// that talks to accounts.spotify.com (both the refresh-token exchange and the
// console's authorization-code exchange, which the console forwards rather
// than performing itself so the secret never reaches a browser-facing app).

// AppSetRequest stores (or replaces) the broadcaster's own Spotify application
// credentials. Both fields are required: half an app cannot authenticate.
type AppSetRequest struct {
	UserID       string `json:"user_id"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// AppClearRequest removes the stored application credentials.
type AppClearRequest struct {
	UserID string `json:"user_id"`
}

// AppStatusRequest asks whether an application is on file.
type AppStatusRequest struct {
	UserID string `json:"user_id"`
}

// AppStatusReply reports presence plus the client id: public by construction,
// and the console shows it so a broadcaster can tell WHICH of their Spotify
// apps is wired up. The secret is never echoed.
type AppStatusReply struct {
	Present  bool   `json:"present"`
	ClientID string `json:"client_id,omitempty"`
	Error    string `json:"error,omitempty"`
}
