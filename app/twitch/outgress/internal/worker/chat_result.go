// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"io"
	"net/http"

	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

func (w *Worker) recordChatResult(payload *outgress.Message, res *http.Response) {
	if payload.Type != outgress.TypeChat || res.StatusCode != http.StatusOK {
		return
	}
	var reply struct {
		Data []struct {
			IsSent     *bool `json:"is_sent"`
			DropReason *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"drop_reason"`
		} `json:"data"`
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseDrain))
	if err != nil || codec.Unmarshal(body, &reply) != nil {
		return
	}
	if len(reply.Data) == 0 {
		return
	}
	if sent := reply.Data[0].IsSent; sent != nil && !*sent {
		w.analytics.Count(catalog.OutgressDropped, "chat", "chat_dropped")
	}
}
