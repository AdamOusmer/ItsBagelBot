// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"io"
	"net/http"

	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

type chatDeliveryKey struct{}

type chatDelivery struct{ sent bool }

func (w *Worker) recordChatResult(ctx context.Context, payload *outgress.Message, res *http.Response) {
	if payload.Type != outgress.TypeChat || res.StatusCode != http.StatusOK {
		return
	}
	sent := readChatDelivery(res.Body)
	if sent == nil {
		return
	}
	if delivery, ok := ctx.Value(chatDeliveryKey{}).(*chatDelivery); ok {
		delivery.sent = *sent
	}
	if !*sent {
		w.analytics.Count(catalog.OutgressDropped, "chat", "chat_dropped")
	}
}

func readChatDelivery(r io.Reader) *bool {
	var reply struct {
		Data []struct {
			IsSent *bool `json:"is_sent"`
		} `json:"data"`
	}
	body, err := io.ReadAll(io.LimitReader(r, maxResponseDrain))
	if err != nil || codec.Unmarshal(body, &reply) != nil {
		return nil
	}
	if len(reply.Data) == 0 {
		return nil
	}
	return reply.Data[0].IsSent
}
