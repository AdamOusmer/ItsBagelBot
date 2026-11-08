// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package modulesrpc

import "ItsBagelBot/internal/domain/rpc"

type CountdownRequest struct {
	UserID      string `json:"user_id"`
	Name        string `json:"name"`
	Seconds     int64  `json:"seconds,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
}

func (r CountdownRequest) Requested() string { return r.UserID }

type CountdownReply struct {
	Found         bool  `json:"found,omitempty"`
	Capped        bool  `json:"capped,omitempty"`
	LimitExceeded bool  `json:"limit_exceeded,omitempty"`
	ExpiresAtMS   int64 `json:"expires_at_ms,omitempty"`
	rpc.Refusal
}
type CountdownDelivery struct {
	UserID      string            `json:"user_id"`
	Name        string            `json:"name"`
	Generation  string            `json:"generation"`
	Token       string            `json:"token"`
	ExpiresAtMS int64             `json:"expires_at_ms"`
	Progress    map[string]string `json:"progress,omitempty"`
}
type CountdownDeliveryRequest struct {
	Delivery CountdownDelivery `json:"delivery,omitempty"`
}
type CountdownDeliveryReply struct {
	Found    bool              `json:"found,omitempty"`
	Delivery CountdownDelivery `json:"delivery,omitempty"`
	rpc.Refusal
}

type CountdownProgressRequest struct {
	Delivery CountdownDelivery `json:"delivery"`
	Key      string            `json:"key"`
	Status   string            `json:"status"`
}
