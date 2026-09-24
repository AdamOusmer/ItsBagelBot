// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotifyrpc

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
