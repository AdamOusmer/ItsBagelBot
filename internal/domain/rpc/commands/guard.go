// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commandsrpc

// Requested lets the dashboard verbs ride bus.ServeForUser, which owns the
// user-id guard the whole fleet shares; the reply half (Failed) comes from the
// rpc.Refusal DashboardReply embeds.

func (r DashboardRequest) Requested() string { return r.UserID }
