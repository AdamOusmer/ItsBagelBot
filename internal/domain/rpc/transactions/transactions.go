// Package transactionsrpc holds the shared wire types for the transactions
// service RPC surface, so consumers can reference them without pulling in the
// full service.
package transactionsrpc

// BasketCreateRequest asks the transactions service to mint a Tebex Headless
// basket for the premium package, tagged with the buyer's Twitch user id so the
// payment webhook can attribute the entitlement.
type BasketCreateRequest struct {
	UserID   string `json:"user_id"`
	Username string `json:"username,omitempty"`
}

type BasketCreateReply struct {
	// Ident is the basket identifier Tebex.js launches checkout with.
	Ident string `json:"ident,omitempty"`
	// CheckoutURL is the hosted-checkout link for the same basket, kept as a
	// fallback when the embedded checkout cannot run.
	CheckoutURL string `json:"checkout_url,omitempty"`
	Error       string `json:"error,omitempty"`
}
