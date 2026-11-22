// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"go.uber.org/zap"

	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
)

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
