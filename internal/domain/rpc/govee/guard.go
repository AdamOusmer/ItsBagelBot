// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package goveerpc

// Requested and Failed let every verb on these subjects ride bus.ServeForUser,
// which owns the user-id guard the whole fleet shares. Structural interfaces,
// so this package still imports nothing.

func (r KeySetRequest) Requested() string    { return r.UserID }
func (r KeyClearRequest) Requested() string  { return r.UserID }
func (r KeyStatusRequest) Requested() string { return r.UserID }
func (r KeyGetRequest) Requested() string    { return r.UserID }

func (r *KeyMutateReply) Failed(message string) { r.Error = message }
func (r *KeyStatusReply) Failed(message string) { r.Error = message }
func (r *KeyGetReply) Failed(message string)    { r.Error = message }
