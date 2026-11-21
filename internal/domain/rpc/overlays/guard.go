// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlaysrpc

func (r PoweredGetRequest) Requested() string    { return r.UserID }
func (r PoweredSaveRequest) Requested() string   { return r.UserID }
func (r PoweredRotateRequest) Requested() string { return r.UserID }
