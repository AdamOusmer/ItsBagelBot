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
	// Automod is the inline chat guard. nil disables it; when set it inspects
	// each chat line and the engine acts on or shadow-logs the verdict.
	Automod *automod.Gate
	// Reputation is the per-chatter strike store: it feeds the automod's Tier-2
	// escalation and is fed by the folded-cohort fan-out. nil disables it.
	Reputation Reputation
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

// NoopCooldown never gates: every call is allowed. Used in tests and when no
// cooldown backend is configured.
type NoopCooldown struct{}

func (NoopCooldown) Allow(context.Context, string, time.Duration) (bool, error) { return true, nil }

// NoopDedup never folds deliveries. It keeps tests and alternate embeddings
// working when no shared dedup backend is configured.
type NoopDedup struct{}

func (NoopDedup) Claim(context.Context, string) (bool, error) { return true, nil }
func (NoopDedup) Release(context.Context, string) error       { return nil }
