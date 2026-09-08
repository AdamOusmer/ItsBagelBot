// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fetchkey

// Requested and Failed let every verb on these subjects ride bus.ServeForUser,
// which owns the user-id guard the whole fleet shares. FetchListRequest and
// FetchListReply carry theirs beside their declarations because the projection
// fallback verb needed them first. Structural, so this package still imports
// nothing.

func (r KeyGetRequest) Requested() string      { return r.UserID }
func (r FetchDefSetRequest) Requested() string { return r.UserID }
func (r FetchKeySetRequest) Requested() string { return r.UserID }
func (r FetchDeleteRequest) Requested() string { return r.UserID }

func (r *KeyGetReply) Failed(message string)      { r.Error = message }
func (r *FetchMutateReply) Failed(message string) { r.Error = message }
func (r *FetchKeySetReply) Failed(message string) { r.Error = message }
