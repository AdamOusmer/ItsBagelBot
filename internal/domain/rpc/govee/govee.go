// Package goveerpc holds the shared wire types for the Govee API-key custody
// RPC the modules service owns. The key is a broadcaster secret: it is stored
// encrypted at rest (Tink AEAD, the modules service's own keyset) and never
// leaves the fleet except decrypted to the gateway, the one service that dials
// Govee.
//
// Two subject families, split by trust:
//
//   - Dashboard verbs under a public-ish prefix (default "bagel.rpc.modules.govee"):
//     "set" stores a key, "clear" removes it, "status" reports only whether one
//     is on file. None ever echoes the key back — the console shows "key on
//     file", never the value.
//   - One internal verb, "bagel.rpc.internal.govee.key.get", export/import-scoped
//     at the NATS account level to the gateway alone, mirroring the users
//     service's token/email RPCs. It returns the decrypted key so the gateway
//     can authenticate a control call.
package goveerpc

// KeySetRequest stores (or replaces) a broadcaster's Govee API key.
type KeySetRequest struct {
	UserID string `json:"user_id"`
	Key    string `json:"key"`
}

// KeyClearRequest removes a broadcaster's stored key.
type KeyClearRequest struct {
	UserID string `json:"user_id"`
}

// KeyStatusRequest asks whether a broadcaster has a key on file.
type KeyStatusRequest struct {
	UserID string `json:"user_id"`
}

// KeyStatusReply reports only presence, never the key itself.
type KeyStatusReply struct {
	Present bool   `json:"present"`
	Error   string `json:"error,omitempty"`
}

// KeyMutateReply is the ack for set/clear: a bare error envelope.
type KeyMutateReply struct {
	Error string `json:"error,omitempty"`
}

// KeyGetRequest is the internal decrypt request the gateway makes.
type KeyGetRequest struct {
	UserID string `json:"user_id"`
}

// KeyGetReply carries the decrypted key or a terminal error. An empty Key with
// empty Error means the broadcaster has no key on file yet (the caller treats
// that as "govee not set up", not a failure).
type KeyGetReply struct {
	Key   string `json:"key,omitempty"`
	Error string `json:"error,omitempty"`
}
