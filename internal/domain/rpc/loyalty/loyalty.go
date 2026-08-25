// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package loyaltyrpc holds the shared wire types for the loyalty service RPC
// surface (bagel.rpc.loyalty.*). Sesame reads balances and counters through
// these verbs on a cache miss and writes counter management actions (create,
// set, delete) through them; the future dashboard pages ride the same verbs.
package loyaltyrpc

// Request covers every loyalty verb; unused fields are zero-valued.
type Request struct {
	UserID      string `json:"user_id"`                // broadcaster Twitch id
	ViewerID    string `json:"viewer_id,omitempty"`    // chatter Twitch id (balance.transfer sender)
	ViewerLogin string `json:"viewer_login,omitempty"` // chatter login (balance.set/add/transfer target; counter.set identity stamp)
	Name        string `json:"name,omitempty"`         // counter name
	NewName     string `json:"new_name,omitempty"`     // rename target (counter.rename)
	Scope       string `json:"scope,omitempty"`        // data.CounterScope* (create)
	Command     string `json:"command,omitempty"`      // viewer+command bucket key
	Value       int64  `json:"value,omitempty"`        // absolute value (set) or delta (add)
	Limit       int    `json:"limit,omitempty"`        // top-N size
}

// Balance is one viewer's standing in one channel.
type Balance struct {
	ViewerID     string `json:"viewer_id"`
	ViewerLogin  string `json:"viewer_login,omitempty"`
	ViewerName   string `json:"viewer_name,omitempty"`
	Points       int64  `json:"points"`
	WatchSeconds uint64 `json:"watch_seconds"`
}

// Counter is one counter's definition plus its channel-scope value. A
// viewer-scoped counter's per-viewer value rides Value only on counter.get
// with a viewer_id; list returns the definitions.
type Counter struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
	Value int64  `json:"value"`
}

// CounterEntry is one stored bucket value of an entry-scoped counter, as
// counter.entries returns them (highest first). ViewerLogin is resolved from
// the bucket's own stored identity (refreshed on every bump that carried
// one), falling back to the balances table for rows written before identity
// was stored; empty when neither knows the viewer.
type CounterEntry struct {
	ViewerID    string `json:"viewer_id"`
	ViewerLogin string `json:"viewer_login,omitempty"`
	ViewerName  string `json:"viewer_name,omitempty"`
	Command     string `json:"command,omitempty"`
	Value       int64  `json:"value"`
}

// CounterRank is one channel's place on a fleet-wide counter board, as
// counter.board returns them (highest first). The board crosses broadcasters
// (every channel that ever bumped the named counter), so the row is keyed by
// the owning broadcaster rather than by a viewer.
type CounterRank struct {
	UserID string `json:"user_id"`
	Value  int64  `json:"value"`
}

// Reply is the reply shape for every loyalty verb; verbs fill only their
// fields. A missing row is not an error: balance.get returns a zero Balance,
// counter.get sets Found=false so the caller can distinguish "0" from "no such
// counter".
type Reply struct {
	Balance *Balance `json:"balance,omitempty"`
	// TargetBalance rides balance.transfer: the recipient's standing after
	// the credit, next to the sender's in Balance. Two fields because both
	// sides of the move are interesting to the caller's reply.
	TargetBalance *Balance       `json:"target_balance,omitempty"`
	Top           []Balance      `json:"top,omitempty"`
	Counter       *Counter       `json:"counter,omitempty"`
	Counters      []Counter      `json:"counters,omitempty"`
	Entries       []CounterEntry `json:"entries,omitempty"`
	Board         []CounterRank  `json:"board,omitempty"`
	Found         bool           `json:"found,omitempty"`
	// Spent reports the conditional outcome of balance.spend: the debit
	// applied (true) or was refused for insufficient points (false, with
	// Found carrying whether the viewer exists at all and Balance what they
	// actually hold).
	Spent bool   `json:"spent,omitempty"`
	Error string `json:"error,omitempty"`
}
