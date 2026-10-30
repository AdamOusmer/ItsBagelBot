// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"errors"

	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

type userUpdate struct {
	UserID    string `json:"user_id"`
	UserLogin string `json:"user_login"`
	UserName  string `json:"user_name"`
	EventID   string `json:"event_id"`
}

func (ev userUpdate) valid() bool { return ev.UserID != "" && ev.UserLogin != "" }

// Duplicates are harmless: identity_sync is a no-op once the stored login matches.
func (w *Worker) HandleUserUpdate(msg *bus.Message) error {
	ctx := msg.Context()
	log := monitor.TxnLogger(ctx, w.log)

	var ev userUpdate
	if err := codec.Unmarshal(msg.Payload, &ev); err != nil || !ev.valid() {
		log.Error("dropping malformed user.update event", zap.Error(err))
		return nil
	}
	if w.reauth == nil {
		return nil
	}

	reply, err := w.reauth.SyncIdentity(ctx, usersrpc.IdentitySyncRequest{
		UserID: ev.UserID, Username: ev.UserLogin, DisplayName: ev.UserName,
	})
	if err != nil {
		return identitySyncFailure(log, ev.UserID, err)
	}
	if reply.Renamed {
		log.Info("broadcaster renamed on Twitch",
			zap.String("user_id", ev.UserID), zap.String("login", ev.UserLogin), zap.String("prior_login", reply.PriorUsername))
	}
	return nil
}

// Refusals are final; only transport failures are returned for redelivery.
func identitySyncFailure(log *zap.Logger, userID string, err error) error {
	if bus.NotFoundReply(err) {
		return nil
	}
	var refused bus.RPCReplyError
	if errors.As(err, &refused) {
		log.Warn("identity sync refused", zap.String("user_id", userID), zap.Error(err))
		return nil
	}
	log.Warn("identity sync failed", zap.String("user_id", userID), zap.Error(err))
	return err
}
