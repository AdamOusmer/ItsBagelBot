// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package transactionsrpc

import "ItsBagelBot/internal/domain/rpc"

type BasketCreateRequest struct {
	UserID            string `json:"user_id"`
	Username          string `json:"username,omitempty"`
	RecipientUsername string `json:"recipient_username,omitempty"`
	IPAddress         string `json:"ip_address,omitempty"`
	PackageType       string `json:"package_type,omitempty"`
	GiftMessage       string `json:"gift_message,omitempty"`
}

type BasketCreateReply struct {
	Ident          string `json:"ident,omitempty"`
	CheckoutURL    string `json:"checkout_url,omitempty"`
	RecipientLogin string `json:"recipient_login,omitempty"`
	rpc.Refusal
}
