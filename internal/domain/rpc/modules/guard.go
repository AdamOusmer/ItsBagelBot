// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modulesrpc

func (r DashboardRequest) Requested() string { return r.UserID }
func (r QuoteRequest) Requested() string     { return r.UserID }
func (r VariableRequest) Requested() string  { return r.UserID }
func (r CooldownRequest) Requested() string  { return r.UserID }
