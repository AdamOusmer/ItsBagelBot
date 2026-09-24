// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package goveerpc

func (r KeySetRequest) Requested() string    { return r.UserID }
func (r KeyClearRequest) Requested() string  { return r.UserID }
func (r KeyStatusRequest) Requested() string { return r.UserID }
func (r KeyGetRequest) Requested() string    { return r.UserID }

func (r *KeyMutateReply) Failed(message string) { r.Error = message }
func (r *KeyStatusReply) Failed(message string) { r.Error = message }
func (r *KeyGetReply) Failed(message string)    { r.Error = message }
