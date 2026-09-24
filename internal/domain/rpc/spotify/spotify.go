// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotifyrpc

type RefreshTokenSetRequest struct {
	UserID       string   `json:"user_id"`
	RefreshToken string   `json:"refresh_token"`
	Scopes       []string `json:"scopes,omitempty"`
}

type RefreshTokenClearRequest struct {
	UserID string `json:"user_id"`
}

type RefreshTokenStatusRequest struct {
	UserID string `json:"user_id"`
}

type RefreshTokenStatusReply struct {
	Present bool     `json:"present"`
	Scopes  []string `json:"scopes,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type RefreshTokenMutateReply struct {
	Error string `json:"error,omitempty"`
}

type RefreshTokenRotateRequest struct {
	UserID    string `json:"user_id"`
	PrevToken string `json:"prev_token"`
	NewToken  string `json:"new_token"`
}

type RefreshTokenGetRequest struct {
	UserID string `json:"user_id"`
}

type RefreshTokenGetReply struct {
	RefreshToken string `json:"refresh_token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Error        string `json:"error,omitempty"`
}

type AppSetRequest struct {
	UserID       string `json:"user_id"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type AppClearRequest struct {
	UserID string `json:"user_id"`
}

type AppStatusRequest struct {
	UserID string `json:"user_id"`
}

type AppStatusReply struct {
	Present  bool   `json:"present"`
	ClientID string `json:"client_id,omitempty"`
	Error    string `json:"error,omitempty"`
}
