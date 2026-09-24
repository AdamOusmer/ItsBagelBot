// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fetchkey

func (r KeyGetRequest) Requested() string      { return r.UserID }
func (r FetchDefSetRequest) Requested() string { return r.UserID }
func (r FetchKeySetRequest) Requested() string { return r.UserID }
func (r FetchDeleteRequest) Requested() string { return r.UserID }
