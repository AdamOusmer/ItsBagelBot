// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automationrpc

func (r ListRequest) Requested() string           { return r.UserID }
func (r GetRequest) Requested() string            { return r.UserID }
func (r UpsertRequest) Requested() string         { return r.UserID }
func (r UpgradeAlertRequest) Requested() string   { return r.UserID }
func (r SetEnabledRequest) Requested() string     { return r.UserID }
func (r DowngradeAlertRequest) Requested() string { return r.UserID }
func (r DeleteRequest) Requested() string         { return r.UserID }
func (r ValidateRequest) Requested() string       { return r.UserID }
func (r ProjectionRequest) Requested() string     { return r.UserID }
