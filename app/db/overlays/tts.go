// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/event/data"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
)

func backfillTts(ctx context.Context, nc *nats.Conn, repo *repository.Overlays) error {
	var cursor usersrpc.VoiceExportRequest
	for {
		page, err := bus.RequestJSONTimeout[usersrpc.VoiceExportReply](ctx, nc, usersrpc.VoiceExportSubject, cursor, 3*time.Second)
		if err != nil {
			return err
		}
		if err := importTtsPage(ctx, repo, page); err != nil {
			return err
		}
		if page.NextID == 0 {
			return nil
		}
		cursor.AfterID = page.NextID
	}
}

func importTtsPage(ctx context.Context, repo *repository.Overlays, page usersrpc.VoiceExportReply) error {
	for _, overlay := range page.Overlays {
		err := repo.ImportTTS(ctx, overlay)
		if err != nil {
			return err
		}
	}
	return nil
}

func bannedTts(repo *repository.Overlays, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var ban data.UserBannedDTO
		if err := codec.Unmarshal(msg.Payload, &ban); err != nil {
			log.Warn("invalid user ban event", zap.Error(err))
			return nil
		}
		return repo.RevokeBannedTTS(msg.Context(), ban)
	}
}
