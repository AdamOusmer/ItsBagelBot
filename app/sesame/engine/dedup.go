package engine

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/idempotency"

	"go.uber.org/zap"
)

// Effect discriminators partition one event's claim namespace so two distinct
// non-idempotent effects driven by the same event get independent guards (an
// award and a counter bump on the same redemption must both fire on the first
// pass, not race one claim). CounterEffect appends the counter name because one
// custom-command reply can bump several distinct counters.
const (
	effectUse          = "use"
	EffectEarn         = "earn"
	EffectPointsAdjust = "padjust"
	EffectQuoteAdd     = "quoteadd"
	counterEffectPre   = "cbump:"
)

// CounterEffect is the per-counter dedup discriminator, normalised so the same
// counter maps to one claim regardless of the caller's casing.
func CounterEffect(name string) string {
	return counterEffectPre + NormalizeCounterName(name)
}

// EventDedup guards the specific non-idempotent side effects a replayed event
// would apply twice: the command-use count, loyalty accrual, counter bumps, a
// relative point grant, and quote creation. It is called only at those sites,
// never on the plain-chat firehose — a per-message claim on every chat line
// would turn the firehose into a per-message master write.
//
// It fails OPEN. A Valkey outage or an event with no stable identity yields
// "apply", because dropping a live effect is worse than a rare double-apply
// during an outage. A nil *EventDedup is the kill-switch: every method is
// nil-safe and degrades to "apply, never dedup".
type EventDedup struct {
	store  idempotency.Store
	prefix string
	ttl    time.Duration
	log    *zap.Logger
	dupes  atomic.Int64
}

// NewEventDedup builds a guard over store. prefix is the store's Valkey key
// namespace (the same one NewValkeyStore was given), needed so CounterClaim can
// name the exact key store.Seen would write. A nil store (kill switch off) leaves
// every method failing open.
func NewEventDedup(store idempotency.Store, prefix string, ttl time.Duration, log *zap.Logger) *EventDedup {
	if log == nil {
		log = zap.NewNop()
	}
	return &EventDedup{store: store, prefix: prefix, ttl: ttl, log: log}
}

// EventIdentity is the stable in-payload key: the chat message id, or the
// EventSub event id for non-chat events. Both are byte-stable across a JetStream
// redelivery AND the schedule-retry hop (the retry copies the payload verbatim),
// so a replay is recognised entirely inside sesame with no dependence on a
// transport header the ingress publisher never stamps. Empty when neither is
// present, which the guard treats as fail-open (never invent an identity).
func EventIdentity(env *lane.Envelope) string {
	if env.MsgID != "" {
		return env.MsgID
	}
	return env.EventID
}

// EffectRef names one guarded effect of one event: the event's stable identity
// and the effect discriminator partitioning its claim namespace. Carried as one
// value so the pair that claims is byte-for-byte the pair that releases.
type EffectRef struct {
	Identity string
	Effect   string
}

// Duplicate claims ref and reports whether it was ALREADY held — a replay whose
// effect must be skipped. It never releases the claim; use it for
// loss-tolerant, fire-and-forget effects (the reporter deltas) where a
// guard-then-apply that a crash could lose is acceptable. Fails OPEN (returns
// false = apply).
func (d *EventDedup) Duplicate(ctx context.Context, ref EffectRef) bool {
	if !d.active(ref.Identity) {
		return false
	}
	seen, _ := d.store.Seen(ctx, dedupKey(ref), d.ttl)
	if seen {
		d.dupes.Add(1)
	}
	return seen
}

// Claim is Duplicate plus a release handle, for effects that must not be
// permanently suppressed by a first-attempt failure. On a fresh claim (dup
// false) the caller runs the effect and calls release() if it then errors, so a
// redelivery re-runs it. On a duplicate or a fail-open admission, release is a
// no-op.
func (d *EventDedup) Claim(ctx context.Context, ref EffectRef) (dup bool, release func()) {
	noop := func() {}
	if !d.active(ref.Identity) {
		return false, noop
	}
	key := dedupKey(ref)
	seen, err := d.store.Seen(ctx, key, d.ttl)
	if seen {
		d.dupes.Add(1)
		return true, noop
	}
	if err != nil {
		return false, noop // failed open: admitted without a claim to release
	}
	return false, func() { _ = d.store.Release(ctx, key) }
}

// CounterClaim names the exact Valkey key (and its TTL) a folded counter-bump
// must claim atomically with the increment: the SAME key store.Seen writes for
// (identity, CounterEffect(name)), so the split-guard path and the folded path
// recognise one another's claim. It returns "" when the guard is inactive — a
// nil guard (kill switch), no store, or an identity-less event — so the folded
// bump writes no claim and simply applies, the same fail-open the split guard
// gives. Folding claim+increment into one script closes the crash window the
// split Claim-then-Bump left open: a redelivery can no longer find the claim set
// but the counter un-incremented.
func (d *EventDedup) CounterClaim(env *lane.Envelope, name string) (string, time.Duration) {
	identity := EventIdentity(env)
	if !d.active(identity) {
		return "", 0
	}
	return idempotency.Key(d.prefix, dedupKey(EffectRef{Identity: identity, Effect: CounterEffect(name)})), d.ttl
}

// Duplicates is the running count of skipped replays, for logging/telemetry.
func (d *EventDedup) Duplicates() int64 {
	if d == nil {
		return 0
	}
	return d.dupes.Load()
}

// active reports whether the guard should consult its store: an enabled guard
// with a store and a non-empty identity. Nil-safe so a nil *EventDedup (kill
// switch) and an identity-less event both fail open.
func (d *EventDedup) active(identity string) bool {
	return d != nil && d.store != nil && identity != ""
}

func dedupKey(ref EffectRef) string {
	return ref.Identity + ":" + ref.Effect
}

// CounterTarget names the one counter a peek reads: the channel's counter by
// name, plus the viewer and command buckets its scope may select (each ignored
// when the scope does not use it). It is LoyaltyStore.CounterPeek's identifying
// tuple carried as one value, so a replayed bump names its counter the same way
// the bump that produced the value did.
type CounterTarget struct {
	BroadcasterID uint64
	Name          string
	ViewerID      uint64
	Command       string
}

// CounterPeekValue reads a counter's current value without incrementing it — the
// value a REPLAYED bump should render instead of double-incrementing. Empty when
// unknown, which renders the same as an unbound counter token.
func CounterPeekValue(ctx context.Context, s LoyaltyStore, target CounterTarget) string {
	c, found, err := s.CounterPeek(ctx, target.BroadcasterID, target.Name, target.ViewerID, target.Command)
	if err != nil || !found {
		return ""
	}
	return strconv.FormatInt(c.Value, 10)
}
