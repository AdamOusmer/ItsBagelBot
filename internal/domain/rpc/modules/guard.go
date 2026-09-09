// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modulesrpc

// Requested lets the dashboard and quote verbs ride bus.ServeForUser, which
// owns the user-id guard the whole fleet shares; the reply half (Failed) comes
// from the rpc.Refusal those replies embed.

func (r DashboardRequest) Requested() string { return r.UserID }
func (r QuoteRequest) Requested() string     { return r.UserID }
