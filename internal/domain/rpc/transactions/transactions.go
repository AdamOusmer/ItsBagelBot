// Package transactionsrpc holds the shared wire types for the transactions
// service RPC surface, so consumers can reference them without pulling in the
// full service.
package transactionsrpc

// BasketCreateRequest asks the transactions service to mint a Tebex Headless
// basket for the premium package. UserID/Username identify the signed-in buyer.
// When RecipientUsername is set the purchase is a gift: the service resolves
// that Twitch login against the users service (must be a registered, non-banned
// account without premium) and the entitlement lands on the recipient while the
// buyer pays.
type BasketCreateRequest struct {
	UserID            string `json:"user_id"`
	Username          string `json:"username,omitempty"`
	RecipientUsername string `json:"recipient_username,omitempty"`
	IPAddress         string `json:"ip_address,omitempty"`
}

type BasketCreateReply struct {
	// Ident is the basket identifier Tebex.js launches checkout with.
	Ident string `json:"ident,omitempty"`
	// CheckoutURL is the Tebex-hosted checkout link for the basket; the
	// dashboard redirects the browser there.
	CheckoutURL string `json:"checkout_url,omitempty"`
	// RecipientLogin echoes the resolved gift recipient (gift baskets only).
	RecipientLogin string `json:"recipient_login,omitempty"`
	Error          string `json:"error,omitempty"`
}
