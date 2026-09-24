// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

func (r TokensRequest) Requested() string   { return r.UserID }
func (r EmailGetRequest) Requested() string { return r.UserID }
