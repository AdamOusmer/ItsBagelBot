// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotifyrpc

// Requested and Failed let every verb on these subjects ride bus.ServeForUser.
// They replace a pair of package-local helpers (spotifyMutate / spotifyRead)
// that reimplemented the same guard with an envelope-constructor callback
// instead, because a generic cannot fill in a field it does not know about --
// which is exactly what the Failing interface solves. Structural, so this
// package still imports nothing.

func (r RefreshTokenSetRequest) Requested() string    { return r.UserID }
func (r RefreshTokenClearRequest) Requested() string  { return r.UserID }
func (r RefreshTokenStatusRequest) Requested() string { return r.UserID }
func (r RefreshTokenRotateRequest) Requested() string { return r.UserID }
func (r RefreshTokenGetRequest) Requested() string    { return r.UserID }
func (r AppSetRequest) Requested() string             { return r.UserID }
func (r AppClearRequest) Requested() string           { return r.UserID }
func (r AppStatusRequest) Requested() string          { return r.UserID }

func (r *RefreshTokenMutateReply) Failed(message string) { r.Error = message }
func (r *RefreshTokenStatusReply) Failed(message string) { r.Error = message }
func (r *RefreshTokenGetReply) Failed(message string)    { r.Error = message }
func (r *AppStatusReply) Failed(message string)          { r.Error = message }
