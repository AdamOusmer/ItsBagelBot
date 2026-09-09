// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

// Requested lets the token and contact-email verbs ride bus.ServeForUser,
// which owns the user-id guard the whole fleet shares. The reply half of that
// guard (Failed) comes from the rpc.Refusal these replies embed.
// AdminRequest deliberately has no Requested: its user id is optional (a
// lookup may arrive by username instead), so its guard cannot be a bind-time
// prologue and stays in the handler, over bus.UserID.

func (r TokensRequest) Requested() string   { return r.UserID }
func (r EmailGetRequest) Requested() string { return r.UserID }
