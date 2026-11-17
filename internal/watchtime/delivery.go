// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type watchDelivery struct {
	entry valkey.XRangeEntry
	award data.WatchAwardDTO
	err   error
}

func watchDeliveryGroups(entries []valkey.XRangeEntry) map[uint64][]watchDelivery {
	groups := make(map[uint64][]watchDelivery)
	for _, entry := range entries {
		award, err := decodeWatchDelivery(entry)
		groups[award.UserID] = append(groups[award.UserID], watchDelivery{entry: entry, award: award, err: err})
	}
	return groups
}

func (c *Consumer) processGroup(ctx context.Context, deliveries []watchDelivery) {
	for _, delivery := range deliveries {
		c.process(ctx, delivery)
	}
}

func (c *Consumer) process(parent context.Context, delivery watchDelivery) {
	ctx, cancel := context.WithTimeout(parent, awardTimeout)
	defer cancel()
	deliveryOwner := pkgvalkey.StreamDelivery{Stream: Stream, Group: ConsumerGroup, Consumer: c.name, ID: delivery.entry.ID}
	owned, err := deliveryOwner.Refresh(ctx, c.client)
	if err != nil {
		c.log.Warn("watchtime: ownership refresh failed; delivery retained", zap.String("stream_id", delivery.entry.ID), zap.Error(err))
		return
	}
	if !owned {
		return
	}
	if delivery.err != nil {
		c.quarantineInvalid(ctx, delivery.entry, delivery.err)
		return
	}
	c.commitDelivery(ctx, delivery.entry, delivery.award)
}

func decodeWatchDelivery(entry valkey.XRangeEntry) (data.WatchAwardDTO, error) {
	var award data.WatchAwardDTO
	if err := codec.Unmarshal([]byte(entry.FieldValues["payload"]), &award); err != nil {
		return award, err
	}
	if err := ValidateAward(award); err != nil {
		return award, err
	}
	if entry.FieldValues["operation_id"] != OperationID(award) {
		return award, errors.New("watch operation identity mismatch")
	}
	return award, nil
}

func (c *Consumer) quarantineInvalid(ctx context.Context, entry valkey.XRangeEntry, cause error) {
	if err := c.quarantine(ctx, entry, cause); err != nil {
		c.log.Error("watchtime: quarantine failed; delivery retained", zap.String("stream_id", entry.ID), zap.Error(err))
		return
	}
	c.log.Error("watchtime: malformed award quarantined", zap.String("stream_id", entry.ID), zap.Error(cause))
}

func (c *Consumer) commitDelivery(ctx context.Context, entry valkey.XRangeEntry, award data.WatchAwardDTO) {
	fields := watchDeliveryFields(entry, award)
	started := time.Now()
	err := c.handle(ctx, award)
	fields = append(fields, zap.Duration("sql_commit_duration", time.Since(started)))
	if err != nil {
		c.log.Warn("watchtime: award commit failed; delivery retained", append(fields, zap.Error(err))...)
		return
	}
	c.log.Debug("watchtime: award committed", fields...)
	if err := c.ack(ctx, entry.ID, award); err != nil {
		c.log.Warn("watchtime: committed award acknowledgement failed; replay is safe", append(fields, zap.Error(err))...)
	}
}

func watchDeliveryFields(entry valkey.XRangeEntry, award data.WatchAwardDTO) []zap.Field {
	fields := []zap.Field{zap.Uint64("broadcaster_id", award.UserID), zap.String("operation_id", OperationID(award)), zap.String("stream_id", entry.ID), zap.String("window_id", award.WindowID), zap.Uint32("chunk", award.Chunk), zap.Int("viewer_count", len(award.Entries))}
	if ms, err := strconv.ParseInt(strings.SplitN(entry.ID, "-", 2)[0], 10, 64); err == nil {
		fields = append(fields, zap.Int64("outbox_delivery_age_ms", max(0, time.Now().UnixMilli()-ms)))
	}
	if start := WindowStartUnixMilli(award); start > 0 {
		fields = append(fields, zap.Int64("window_age_ms", max(0, time.Now().UnixMilli()-start)))
	}
	return fields
}
