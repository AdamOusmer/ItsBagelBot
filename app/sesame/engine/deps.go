// Package engine is sesame's runtime: it takes the immutable module.Module
// values a Builder produced, indexes them in a Registry, and runs the
// interested ones for each message in the consumer's own goroutine. It owns the
// per-message orchestration (decode, command dispatch, event handlers, publish)
// and the shared command gate (permission, live-only, cooldown). The behavior
// lives in the modules (app/sesame/modules); the engine only wires and runs
// them.
//
// The command router is not a module here (as it was in the worker): dispatch is
// an engine stage that reads the registry's command index directly, which
// removes the worker's CommandRouter.Bind init-order footgun.
package engine

import (
	"context"
	"time"

	"ItsBagelBot/app/sesame/automod"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/projection"

	"github.com/ThreeDotsLabs/watermill/message"

	"go.uber.org/zap"
)

// CommandManager is the write side of the custom commands store. The read side
// lives in projection.Reader.Command; this interface lets a module (the cmd
// module) create, update and delete commands by calling the commands service's
// dashboard RPC over NATS.
type CommandManager interface {
	Upsert(ctx context.Context, userID string, name, response string) error
	Delete(ctx context.Context, userID string, name string) error
}

// QuotesStore is the channel-quotes surface behind the quotes module. The
// modules service owns the rows (bagel.rpc.modules.quote.*); QuotesRPC
// implements it. found=false on Get/Random means no such quote / none saved,
// on Remove that the number did not exist.
type QuotesStore interface {
	QuoteAdd(ctx context.Context, broadcasterID uint64, text, addedBy string) (modulesrpc.Quote, error)
	QuoteGet(ctx context.Context, broadcasterID, number uint64) (modulesrpc.Quote, bool, error)
	QuoteRandom(ctx context.Context, broadcasterID uint64) (modulesrpc.Quote, bool, error)
	QuoteRemove(ctx context.Context, broadcasterID, number uint64) (bool, error)
}

// Deps is the bundle of runtime services a module fn captures by closure when it
// builds its Module. main constructs it once and hands it to modules.All. Not
// every module uses every field; unused ones are harmless.
type Deps struct {
	Proj     projection.Reader
	Live     LiveStore
	Greet    GreetStore
	Cooldown CooldownStore
	Dedup    DedupStore
	Special  *SpecialSet
	Pub      message.Publisher
	Commands CommandManager
	Gateway  GatewayCaller
	Log      *zap.Logger
	// Timers arms/disarms a broadcaster's repeating chat-message timers for the
	// length of one stream; ValkeyTimerStore is the default. nil disables it (the
	// live module's stream.online/offline hooks skip the calls).
	Timers TimersStore
	// Automod is the inline chat guard. nil disables it; when set it inspects
	// each chat line and the engine acts on or shadow-logs the verdict.
	Automod *automod.Gate
	// Reputation is the per-chatter strike store: it feeds the automod's Tier-2
	// escalation and is fed by the folded-cohort fan-out. nil disables it.
	Reputation Reputation
	// Campaign is the council's cross-sender juror: distinct-sender counts per
	// near-duplicate template (SimHash bands in valkey). nil disables it.
	Campaign Campaign
	// Queue is the per-broadcaster play queue behind the queue module. nil
	// leaves the module's commands inert.
	Queue QueueStore
	// Quotes is the channel-quotes store behind the quotes module. nil leaves
	// the module's commands inert.
	Quotes QuotesStore
	// Loyalty is the points-and-counters surface behind the loyalty module,
	// the channel-points counter bindings and the {counter:...} response
	// token. nil disables all of them.
	Loyalty LoyaltyStore
	// LoyaltyTick arms/disarms a broadcaster's watch tick for the length of one
	// stream (the loyalty module's viewtime clock); ValkeyLoyaltyClock is the
	// default. nil disables it.
	LoyaltyTick LoyaltyTicker
	// PublicBaseURL is the origin of the public console pages; the !cmd module
	// builds the channel command-page link from it. Empty falls back to the
	// production dashboard origin.
	PublicBaseURL string
}

// IsLiveChecker is the read-only slice of the live store: just "is this
// broadcaster live?". The command gate's live-only check and the bagel greeter
// depend on this narrow interface (ISP) rather than the full LiveStore.
type IsLiveChecker interface {
	IsLive(ctx context.Context, broadcasterID uint64) (bool, error)
}

// LiveStore answers and maintains a broadcaster's live state. Reads are served
// from a cache fronting Valkey with a projector RPC fallback; writes flow from
// the stream events the worker consumes.
type LiveStore interface {
	IsLiveChecker
	SetLive(ctx context.Context, broadcasterID uint64) error
	ClearLive(ctx context.Context, broadcasterID uint64) error
}

// GreetStore tracks which special users have already been greeted in the current
// stream, so the bagel reply fires only on a user's first message per stream.
type GreetStore interface {
	FirstGreet(ctx context.Context, broadcasterID uint64, chatterID string) (bool, error)
	ResetGreets(ctx context.Context, broadcasterID uint64) error
}

// CooldownStore gates an action behind a shared cooldown window. Allow returns
// true when the caller may proceed (the window was free and is now claimed) and
// false while a previous claim is still cooling down.
type CooldownStore interface {
	Allow(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

// DedupStore folds replicated ingress deliveries by claiming an EventSub id for
// long enough that overlapping publishers cannot re-run the same notification.
type DedupStore interface {
	Claim(ctx context.Context, key string) (bool, error)
	Release(ctx context.Context, key string) error
}

// LoyaltyStore is the loyalty surface modules and the pipeline depend on:
// point accrual (fire-and-forget through the worker-side reporter), counter
// bumps/reads over the Valkey live view, cached balance peeks and the
// authoritative counter management verbs. ValkeyLoyaltyStore is the default.
type LoyaltyStore interface {
	// Earn records one viewer's point/watch accrual; batched and loss-tolerant.
	Earn(broadcasterID, viewerID uint64, login, name string, points int64, watchSeconds uint64)
	// CounterBump increments a counter by delta and returns the new value.
	// viewerID is the acting chatter and command the triggering source's name
	// (a command's canonical trigger, or a channel-point reward's title); each
	// is used only when the counter's scope needs it (viewer, or
	// viewer+command — the three modes, all per channel).
	CounterBump(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string, delta int64) (int64, error)
	// CounterPeek reads a counter without bumping it; found=false means it
	// does not exist.
	CounterPeek(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string) (loyaltyrpc.Counter, bool, error)
	// BalanceGet returns one viewer's standing (zero-valued when unseen).
	BalanceGet(ctx context.Context, broadcasterID, viewerID uint64) (loyaltyrpc.Balance, error)
	// BalanceAdjust writes a viewer's points by login (mod grants): absolute
	// sets, otherwise value is a delta. found=false = login never seen here.
	BalanceAdjust(ctx context.Context, broadcasterID uint64, viewerLogin string, value int64, absolute bool) (loyaltyrpc.Balance, bool, error)
	// CounterCreate/CounterSet/CounterDelete/CounterList are the authoritative
	// management verbs behind !counter (and the future dashboard).
	CounterCreate(ctx context.Context, broadcasterID uint64, name, scope string) (loyaltyrpc.Counter, error)
	CounterSet(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string, value int64) (bool, error)
	CounterDelete(ctx context.Context, broadcasterID uint64, name string) error
	CounterList(ctx context.Context, broadcasterID uint64) ([]loyaltyrpc.Counter, error)
}

// LoyaltyTicker arms and disarms a broadcaster's watch tick for the length of
// one stream. Both calls are fire-and-forget, mirroring TimersStore.
type LoyaltyTicker interface {
	Arm(ctx context.Context, broadcasterID uint64)
	Disarm(ctx context.Context, broadcasterID uint64)
}

// TimersStore arms and disarms a broadcaster's repeating chat-message timers
// for the length of one stream. Both calls are fire-and-forget from the
// caller's perspective (no error to act on): a failure is logged by the store
// itself and, at worst, delays a timer starting or stopping until the next
// stream event or expiry.
type TimersStore interface {
	ArmAll(ctx context.Context, broadcasterID uint64)
	DisarmAll(ctx context.Context, broadcasterID uint64)
}

// NoopCooldown never gates: every call is allowed. Used in tests and when no
// cooldown backend is configured.
type NoopCooldown struct{}

func (NoopCooldown) Allow(context.Context, string, time.Duration) (bool, error) { return true, nil }

// NoopDedup never folds deliveries. It keeps tests and alternate embeddings
// working when no shared dedup backend is configured.
type NoopDedup struct{}

func (NoopDedup) Claim(context.Context, string) (bool, error) { return true, nil }
func (NoopDedup) Release(context.Context, string) error       { return nil }
