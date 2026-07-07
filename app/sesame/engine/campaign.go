package engine

import (
	"context"
	"strconv"
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
type Campaign interface {
	// Observe records senderID against both bands of the line's SimHash and
	// returns the largest distinct-sender count seen for either band.
	Observe(ctx context.Context, simhash uint64, senderID string) int
}

// NoopCampaign disables the juror (tests, or valkey absent).
type NoopCampaign struct{}

func (NoopCampaign) Observe(context.Context, uint64, string) int { return 0 }

// campaignWindow is how long a template band accumulates distinct senders. A
// spam wave is minutes, not hours; short keys keep the keyspace tiny.
const campaignWindow = 10 * time.Minute

// ValkeyCampaign counts distinct senders per SimHash band in valkey HyperLogLogs
// at am:tmpl:<band-hex> (fleet-wide, ~12KB worst case per hot key, TTL'd).
// Fully best-effort: any error reads as count 0 and the message path proceeds.
type ValkeyCampaign struct {
	client valkey.Client
	log    *zap.Logger
}

func NewValkeyCampaign(client valkey.Client, log *zap.Logger) *ValkeyCampaign {
	return &ValkeyCampaign{client: client, log: log}
}

func campaignKey(band uint64) string {
	return "am:tmpl:" + strconv.FormatUint(band, 16)
}

func (c *ValkeyCampaign) Observe(ctx context.Context, simhash uint64, senderID string) int {
	if simhash == 0 || senderID == "" {
		return 0
	}
	b1, b2 := simhash>>32, simhash&0xffffffff
	k1, k2 := campaignKey(b1), campaignKey(b2)
	ttl := int64(campaignWindow.Seconds())

	// One round trip: add the sender to both band HLLs, refresh their TTLs, and
	// read both counts back.
	resps := c.client.DoMulti(ctx,
		c.client.B().Pfadd().Key(k1).Element(senderID).Build(),
		c.client.B().Pfadd().Key(k2).Element(senderID).Build(),
		c.client.B().Expire().Key(k1).Seconds(ttl).Build(),
		c.client.B().Expire().Key(k2).Seconds(ttl).Build(),
		c.client.B().Pfcount().Key(k1).Build(),
		c.client.B().Pfcount().Key(k2).Build(),
	)
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
