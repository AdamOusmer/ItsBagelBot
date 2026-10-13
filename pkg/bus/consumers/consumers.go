// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package consumers

import (
	"context"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"
)

type Service string

// MySQL timestamps round to whole seconds and pod clocks differ.
const erasureClockSlack = 5 * time.Second

// Rows written after cutoff belong to a re-registered account and must survive.
func OnUserDeleted(svc Service, log *zap.Logger, sweep func(ctx context.Context, userID uint64, cutoff time.Time) error) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		log := monitor.TxnLogger(msg.Context(), log)

		var dto data.UserDeletedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn(string(svc)+": bad user_deleted payload", zap.Error(err))
			return nil
		}
		if err := validate.UserID(dto.UserID); err != nil {
			log.Warn(string(svc)+": invalid user_id in user_deleted", zap.Error(err))
			return nil
		}
		deletedAt := deletionTime(dto, msg)
		if deletedAt.IsZero() {
			log.Warn(string(svc)+": user_deleted without a deletion time", zap.Uint64("user_id", dto.UserID))
			return nil
		}
		if err := sweep(msg.Context(), dto.UserID, deletedAt.Add(erasureClockSlack)); err != nil {
			return err
		}

		log.Info(string(svc)+": deleted all for user", zap.Uint64("user_id", dto.UserID))
		return nil
	}
}

func deletionTime(dto data.UserDeletedDTO, msg *bus.Message) time.Time {
	if dto.DeletedAt.IsZero() {
		return msg.StoredAt()
	}
	return dto.DeletedAt
}

func OnChangeInvalidate[DTO any](userID func(DTO) uint64, invalidate func(uint64)) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto DTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			return err
		}
		invalidate(userID(dto))
		return nil
	}
}
