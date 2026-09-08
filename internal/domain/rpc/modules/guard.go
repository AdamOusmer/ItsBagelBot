// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modulesrpc

// Requested and Failed let the dashboard and quote verbs ride
// bus.ServeForUser, which owns the user-id guard the whole fleet shares.
// Structural, so this package gains no import.

func (r DashboardRequest) Requested() string { return r.UserID }
func (r QuoteRequest) Requested() string     { return r.UserID }

func (r *DashboardReply) Failed(message string) { r.Error = message }
func (r *QuoteReply) Failed(message string)     { r.Error = message }
