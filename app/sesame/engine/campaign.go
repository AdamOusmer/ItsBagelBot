// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// Campaign is the council's cross-sender juror: it groups near-duplicate
// suspicious lines (by SimHash band) and reports how many DISTINCT chatters
// posted the template inside the window. The ingress squash folds byte-identical
// duplicates; this catches the reworded flood (a swapped link, name or emoji per
// message) that exact matching cannot. It is a counting signal the pipeline's
// foreman fuses with a content verdict - never an actor on its own, so communal
// copypasta (which is identical, and folded upstream anyway) is untouched.
//
// Counts are scoped per broadcaster: a quorum is senders within ONE channel.
type Campaign interface {
	// Observe records senderID against both bands of the line's SimHash for the
	// given broadcaster and returns the largest distinct-sender count seen for
	// either band of that tenant.
	Observe(ctx context.Context, broadcasterID uint64, simhash uint64, senderID string) int
}

// NoopCampaign disables the juror (tests, or valkey absent).
type NoopCampaign struct{}

func (NoopCampaign) Observe(context.Context, uint64, uint64, string) int { return 0 }

// campaignWindow is how long a template band accumulates distinct senders. A
// spam wave is minutes, not hours; short keys keep the keyspace tiny.
const campaignWindow = 10 * time.Minute

// campaignErrLogInterval is how often the PFADD/EXPIRE failure debug line may
// repeat. A valkey outage would otherwise emit one line per message (two write
// replies per band pair per line), drowning the debug log in duplicates; 30s
// keeps an outage visible (~2 lines/min) while the suppressed counter carries
// the volume. Fail-open semantics are unaffected: write errors never change the
// returned count.
const campaignErrLogInterval = 30 * time.Second

// ValkeyCampaign counts distinct senders per SimHash band in valkey HyperLogLogs
// at am:tmpl:<broadcasterID>:<band-hex> (~12KB worst case per hot key, TTL'd).
// Fully best-effort: any error reads as count 0 and the message path proceeds.
type ValkeyCampaign struct {
	client valkey.Client
	log    *zap.Logger

	// Write-error visibility: PFADD/EXPIRE failures used to vanish (only
	// PFCOUNT was checked), so a silently dead juror looked healthy. Each
	// failed reply bumps errPending; the debug line fires at most once per
	// campaignErrLogInterval and reports how many were swallowed since.
	errPending     atomic.Int64
	lastWriteLogNs atomic.Int64
}

func NewValkeyCampaign(client valkey.Client, log *zap.Logger) *ValkeyCampaign {
	return &ValkeyCampaign{client: client, log: log}
}

func campaignKey(broadcasterID uint64, band uint64) string {
	return "am:tmpl:" + strconv.FormatUint(broadcasterID, 10) + ":" + strconv.FormatUint(band, 16)
}

// simBands splits a SimHash into two 32-bit bands, exactly like the unexported
// automod/simhash.go simBands. Duplicated rather than exported upstream because
// the automod package is owned separately; if the two ever diverge, bands are
// computed from different halves of the hash and quorums silently split.
func simBands(h uint64) (uint64, uint64) {
	return h >> 32, h & 0xffffffff
}

func (c *ValkeyCampaign) Observe(ctx context.Context, broadcasterID uint64, simhash uint64, senderID string) int {
	if broadcasterID == 0 || simhash == 0 || senderID == "" {
		return 0
	}
	b1, b2 := simBands(simhash)
	k1, k2 := campaignKey(broadcasterID, b1), campaignKey(broadcasterID, b2)
	ttl := int64(campaignWindow.Seconds())

	// One round trip: add the sender to both band HLLs, refresh their TTLs, and
	// read both counts back.
	//
	// The EXPIRE is a sliding TTL, kept deliberately over a fixed first-seen
	// expiry: refreshing on every observation lets a slow-trickle raid (one new
	// sender every few minutes, spaced wider than any fixed window) keep its
	// band alive across the gaps and still reach quorum, while a fixed window
	// would reset the count mid-raid. The cost is bounded: an HLL tops out at
	// ~12KB per key no matter how many senders pile in, and the window still
	// bounds how long an abandoned band lingers.
	resps := c.client.DoMulti(ctx,
		c.client.B().Pfadd().Key(k1).Element(senderID).Build(),
		c.client.B().Pfadd().Key(k2).Element(senderID).Build(),
		c.client.B().Expire().Key(k1).Seconds(ttl).Build(),
		c.client.B().Expire().Key(k2).Seconds(ttl).Build(),
		c.client.B().Pfcount().Key(k1).Build(),
		c.client.B().Pfcount().Key(k2).Build(),
	)

	// Migration: these keys replaced the fleet-wide am:tmpl:<band> ones, under
	// which unrelated channels posting the same meme template fused into one
	// quorum. Old keys carry no state worth keeping; they expire through the
	// unchanged campaignWindow TTL, so there is no cleanup pass.
	c.noteWriteErrors(resps[:4])

	max := 0
	for _, r := range resps[4:] {
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

// noteWriteErrors sweeps the four write replies (PFADD x2, EXPIRE x2) so a
// failing backend shows up in the debug log instead of failing invisibly. It
// never alters the message path: the caller has already read the counts.
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
	c.errPending.Store(0) // best-effort reset; a concurrent bump may be miscounted
}
