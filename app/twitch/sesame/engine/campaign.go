// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"ItsBagelBot/pkg/cache"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type Campaign interface {
	Observe(ctx context.Context, broadcasterID uint64, simhash uint64, senderID string) int
}

type NoopCampaign struct{}

func (NoopCampaign) Observe(context.Context, uint64, uint64, string) int { return 0 }

const campaignWindow = 10 * time.Minute

const campaignErrLogInterval = 30 * time.Second

type ValkeyCampaign struct {
	client valkey.Client
	log    *zap.Logger

	errPending     atomic.Int64
	lastWriteLogNs atomic.Int64
}

func NewValkeyCampaign(client valkey.Client, log *zap.Logger) *ValkeyCampaign {
	return &ValkeyCampaign{client: client, log: log}
}

func campaignKey(broadcasterID uint64, band uint64) string {
	return cache.PairKey("am:tmpl:", broadcasterID, strconv.FormatUint(band, 16))
}

func simBands(h uint64) (uint64, uint64) {
	return h >> 32, h & 0xffffffff
}

func campaignInputUsable(broadcasterID, simhash uint64, senderID string) bool {
	return broadcasterID != 0 && simhash != 0 && senderID != ""
}

func (c *ValkeyCampaign) Observe(ctx context.Context, broadcasterID uint64, simhash uint64, senderID string) int {
	if !campaignInputUsable(broadcasterID, simhash, senderID) {
		return 0
	}
	b1, b2 := simBands(simhash)
	k1, k2 := campaignKey(broadcasterID, b1), campaignKey(broadcasterID, b2)
	ttl := int64(campaignWindow.Seconds())

	resps := c.client.DoMulti(ctx,
		c.client.B().Pfadd().Key(k1).Element(senderID).Build(),
		c.client.B().Pfadd().Key(k2).Element(senderID).Build(),
		c.client.B().Expire().Key(k1).Seconds(ttl).Build(),
		c.client.B().Expire().Key(k2).Seconds(ttl).Build(),
		c.client.B().Pfcount().Key(k1).Build(),
		c.client.B().Pfcount().Key(k2).Build(),
	)

	c.noteWriteErrors(resps[:4])

	return c.bandQuorum(resps[4:])
}

func (c *ValkeyCampaign) bandQuorum(resps []valkey.ValkeyResult) int {
	max := 0
	for _, r := range resps {
		n, err := r.AsInt64()
		if err != nil {
			c.log.Debug("campaign pfcount failed", zap.Error(err))
			continue
		}
		if int(n) > max {
			max = int(n)
		}
	}
	return max
}

func (c *ValkeyCampaign) noteWriteErrors(resps []valkey.ValkeyResult) {
	var failed int64
	var first error
	for _, r := range resps {
		if err := r.Error(); err != nil {
			failed++
			if first == nil {
				first = err
			}
		}
	}
	if failed > 0 {
		c.logWriteFailures(failed, first)
	}
}

func (c *ValkeyCampaign) logWriteFailures(n int64, err error) {
	pending := c.errPending.Add(n)
	now := time.Now().UnixNano()
	last := c.lastWriteLogNs.Load()
	if last != 0 && now-last < int64(campaignErrLogInterval) {
		return
	}
	if !c.lastWriteLogNs.CompareAndSwap(last, now) {
		return
	}
	c.log.Debug("campaign hll write failed",
		zap.Int64("suppressed", pending), zap.Error(err))
	c.errPending.Store(0)
}
