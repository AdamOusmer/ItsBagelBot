// Package conduit resolves the active Twitch conduit id that ingress owns,
// via a NATS RPC request. Results are cached for ttl to avoid a round-trip on
// every eventsub job; the cache is invalidated on enroll failures so a drifted
// conduit id is re-fetched on the next attempt.
package conduit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// reply is the wire shape returned by bagel.rpc.ingress.conduit.get.
// RequestJSON normalises {"error":"..."} replies into a Go error before
// unmarshalling, so the Error field here is only populated if the server
// sends both conduit_id and error (which it should not). Normal error
// replies are surfaced as bus.RPCReplyError via the returned Go error.
type reply struct {
	ConduitID string `json:"conduit_id"`
	Error     string `json:"error"`
}

// Resolver fetches and caches the conduit id from ingress over NATS RPC.
// It falls back to a static env value when the RPC is unreachable.
type Resolver struct {
	nc       *nats.Conn
	subject  string
	fallback string
	ttl      time.Duration
	log      *zap.Logger

	mu        sync.Mutex
	cached    string
	fetchedAt time.Time
}

// New creates a Resolver. fallback is the TWITCH_CONDUIT_ID env value used
// when the ingress RPC is unreachable. ttl controls how long a successful
// reply is trusted before re-querying (60s is a sensible default).
func New(nc *nats.Conn, subject, fallback string, ttl time.Duration, log *zap.Logger) *Resolver {
	return &Resolver{
		nc:       nc,
		subject:  subject,
		fallback: fallback,
		ttl:      ttl,
		log:      log,
	}
}

// Get returns the conduit id, fetching from ingress if the cache is stale.
// On RPC failure it returns the stale cached value, then the env fallback,
// then an error if neither is available.
func (r *Resolver) Get(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cached != "" && time.Since(r.fetchedAt) < r.ttl {
		return r.cached, nil
	}

	res, err := bus.RequestJSON[reply](ctx, r.nc, r.subject, struct{}{})
	if err == nil && res.ConduitID != "" && res.Error == "" {
		r.cached = res.ConduitID
		r.fetchedAt = time.Now()
		return r.cached, nil
	}

	// RPC failed or reply was empty/errored. Log and fall through to stale/fallback.
	if err != nil {
		r.log.Warn("conduit rpc failed", zap.String("subject", r.subject), zap.Error(err))
	} else {
		r.log.Warn("conduit rpc returned empty or errored reply",
			zap.String("conduit_id", res.ConduitID),
			zap.String("error", res.Error))
	}

	if r.cached != "" {
		r.log.Warn("serving stale conduit id", zap.String("conduit_id", r.cached))
		return r.cached, nil
	}

	if r.fallback != "" {
		r.log.Warn("using env fallback conduit id", zap.String("conduit_id", r.fallback))
		return r.fallback, nil
	}

	return "", fmt.Errorf("conduit id unavailable: ingress rpc unreachable and no fallback configured")
}

// Invalidate clears the cache so the next Get re-queries ingress. Call this
// when a conduit-related Twitch error suggests the cached id is wrong.
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cached = ""
	r.fetchedAt = time.Time{}
}
