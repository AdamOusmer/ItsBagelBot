// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package riotrpc

func (r LinkSetRequest) Requested() string  { return r.UserID }
func (r LinkUserRequest) Requested() string { return r.UserID }

func (r *LinkReply) Failed(message string)       { r.Error = message }
func (r *LinkMutateReply) Failed(message string) { r.Error = message }
