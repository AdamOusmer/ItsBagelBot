// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fetchkey

// Requested lets every verb on these subjects ride bus.ServeForUser, which
// owns the user-id guard the whole fleet shares. FetchListRequest carries its
// own beside its declaration because the projection fallback verb needed it
// first. The reply half of that guard (Failed) now comes from the embedded
// rpc.Refusal, so the per-reply one-liners that used to sit here are gone.

func (r KeyGetRequest) Requested() string      { return r.UserID }
func (r FetchDefSetRequest) Requested() string { return r.UserID }
func (r FetchKeySetRequest) Requested() string { return r.UserID }
func (r FetchDeleteRequest) Requested() string { return r.UserID }
