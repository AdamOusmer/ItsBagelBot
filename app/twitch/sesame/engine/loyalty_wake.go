// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func (s *ValkeyLoyaltyClock) nudge() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *ValkeyLoyaltyClock) onExpired(_ context.Context, key string) {
	if strings.HasPrefix(key, loyaltyTickKeyPrefix) && !strings.HasPrefix(key, loyaltyTickClaimPrefix) {
		s.nudge()
	}
}

func (s *ValkeyLoyaltyClock) StartExpiryWatcher(ctx context.Context) {
	channel := "__keyevent@" + strconv.Itoa(s.keyspaceDB) + "__:expired"
	for ctx.Err() == nil {
		err := s.client.Receive(ctx, s.client.B().Subscribe().Channel(channel).Build(), func(msg valkey.PubSubMessage) { s.onExpired(ctx, msg.Message) })
		if ctx.Err() != nil {
			return
		}
		s.log.Warn("loyalty: expiry wake watcher disconnected", zap.Error(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (s *ValkeyLoyaltyClock) StartRearmWatcher(ctx context.Context) {
	if s.nc == nil || s.rearmSubject == "" {
		return
	}
	sub, err := s.nc.Subscribe(s.rearmSubject, func(msg *nats.Msg) {
		var dto invalidate.DTO
		if codec.Unmarshal(msg.Data, &dto) != nil {
			return
		}
		if id, err := strconv.ParseUint(dto.BroadcasterID, 10, 64); err == nil && id != 0 {
			select {
			case s.rearms <- id:
			default:
				s.nudge()
			}
		}
	})
	if err != nil {
		s.log.Error("loyalty: rearm watcher unavailable", zap.Error(err))
		return
	}
	defer sub.Unsubscribe()
	<-ctx.Done()
}
