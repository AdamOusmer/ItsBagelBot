package modulesrpc

// FeedBumpRequest asks the modules service to increment the permanent global
// "feed the bagel" counter (bagel.rpc.modules.personality.feed). There is
// nothing to parameterize: the counter is fleet-wide by design.
type FeedBumpRequest struct{}

// FeedBumpReply returns the new lifetime total after the bump.
type FeedBumpReply struct {
	Total uint64 `json:"total"`
	Error string `json:"error,omitempty"`
}
