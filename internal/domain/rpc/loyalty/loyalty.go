// Package loyaltyrpc holds the shared wire types for the loyalty service RPC
// surface (bagel.rpc.loyalty.*). Sesame reads balances and counters through
// these verbs on a cache miss and writes counter management actions (create,
// set, delete) through them; the future dashboard pages ride the same verbs.
package loyaltyrpc

// Request covers every loyalty verb; unused fields are zero-valued.
type Request struct {
	UserID      string `json:"user_id"`                // broadcaster Twitch id
	ViewerID    string `json:"viewer_id,omitempty"`    // chatter Twitch id
	ViewerLogin string `json:"viewer_login,omitempty"` // chatter login (balance.set/add target)
	Name        string `json:"name,omitempty"`         // counter name
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
// the balances table when the viewer has one; empty otherwise.
type CounterEntry struct {
	ViewerID    string `json:"viewer_id"`
	ViewerLogin string `json:"viewer_login,omitempty"`
	Command     string `json:"command,omitempty"`
	Value       int64  `json:"value"`
}

// Reply is the reply shape for every loyalty verb; verbs fill only their
// fields. A missing row is not an error: balance.get returns a zero Balance,
// counter.get sets Found=false so the caller can distinguish "0" from "no such
// counter".
type Reply struct {
	Balance  *Balance       `json:"balance,omitempty"`
	Top      []Balance      `json:"top,omitempty"`
	Counter  *Counter       `json:"counter,omitempty"`
	Counters []Counter      `json:"counters,omitempty"`
	Entries  []CounterEntry `json:"entries,omitempty"`
	Found    bool           `json:"found,omitempty"`
	Error    string         `json:"error,omitempty"`
}
