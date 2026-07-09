package worker

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/ratelimit"
)

// Twitch enforces the chat limits per channel (20 messages per 30s, 100 when
// the bot moderates the channel), one Helix budget for app-access requests,
// and a separate budget per client ID + user for user-access requests.
//
// That 800/min budget is partitioned so the lanes cannot starve each other:
//
//	helixSystemReserve   tokens/min are reserved for the system lane (the
//	                     dashboard's EventSub create/delete jobs), drawn from
//	                     ratelimit:helix:system and nothing else. Reserving
//	                     them means an onboarding burst always has capacity,
//	                     and capping the lane to them means a flood of toggles
//	                     can never drain the budget chat/api traffic needs.
//	helixGeneralCapacity tokens/min (the remainder) back ordinary api traffic
//	                     on ratelimit:helix:app. The standard lane is held to
//	                     half of that by its own bucket, so premium api always
//	                     finds at least half of the general budget free.
//
// The two partitions are disjoint and sum to the real limit, so the fleet
// never exceeds 800/min no matter how the lanes mix.
const (
	chatCapacity    = 20.0
	chatModCapacity = 100.0
	chatWindow      = 30.0

	helixCapacity        = 800.0
	helixWindow          = 60.0
	helixSystemReserve   = 100.0
	helixGeneralCapacity = helixCapacity - helixSystemReserve
	helixUserCapacity    = 800.0
)

// Stable bucket parameters are formatted once at process initialization. Chat
// keys remain per-broadcaster, but their numeric Lua arguments do not.
var (
	chatSpec              = ratelimit.NewSpec(chatCapacity, chatCapacity/chatWindow)
	chatStandardSpec      = ratelimit.NewSpec(chatCapacity/2, chatCapacity/chatWindow/2)
	chatModSpec           = ratelimit.NewSpec(chatModCapacity, chatModCapacity/chatWindow)
	chatModStandardSpec   = ratelimit.NewSpec(chatModCapacity/2, chatModCapacity/chatWindow/2)
	helixGeneralSpec      = ratelimit.NewSpec(helixGeneralCapacity, helixGeneralCapacity/helixWindow)
	helixStandardSpec     = ratelimit.NewSpec(helixGeneralCapacity/2, helixGeneralCapacity/helixWindow/2)
	helixSystemSpec       = ratelimit.NewSpec(helixSystemReserve, helixSystemReserve/helixWindow)
	helixUserSpec         = ratelimit.NewSpec(helixUserCapacity, helixUserCapacity/helixWindow)
	helixUserStandardSpec = ratelimit.NewSpec(helixUserCapacity/2, helixUserCapacity/helixWindow/2)
)

// generalHelixRequests maps a message to the standard and shared bucket
// requests for the token identity it will execute under, mirroring
// twitch.ResolveIdentity so accounting and token selection cannot disagree.
func generalHelixRequests(payload outgress.Message) (standard, shared ratelimit.Request) {
	identity := twitch.ResolveIdentity(twitch.ParseIdentity(payload.As), payload.Endpoint)
	switch identity {
	case twitch.IdentityBot:
		shared = helixUserSpec.ForKey("ratelimit:helix:user:bot")
		standard = helixUserStandardSpec.ForKey("ratelimit:helix:user:bot:standard")
	case twitch.IdentityBroadcaster:
		shared = helixUserSpec.ForDynamicKey("ratelimit:helix:user:", "helix:user", payload.BroadcasterID)
		standard = helixUserStandardSpec.ForDynamicKey("ratelimit:helix:user:standard:", "helix:user:standard", payload.BroadcasterID)
	default:
		shared = helixGeneralSpec.ForKey("ratelimit:helix:app")
		standard = helixStandardSpec.ForKey("ratelimit:helix:app:standard")
	}
	return standard, shared
}

// takeChat pays the per-broadcaster chat buckets, at mod capacity when the bot
// moderates the channel. The standard lane is constrained by BOTH a restricted
// standard bucket and the shared bucket, consumed atomically via takeOrdered:
// a denial on either bucket leaves both untouched, avoiding token waste during
// retry storms.
func (w *Worker) takeChat(ctx context.Context, broadcasterID string, isMod bool) error {
	sharedSpec, standardSpec := chatSpec, chatStandardSpec
	if isMod {
		sharedSpec, standardSpec = chatModSpec, chatModStandardSpec
	}
	shared := sharedSpec.ForDynamicKey("ratelimit:chat:", "chat", broadcasterID)
	if w.lane != LaneStandard {
		return w.take(ctx, shared)
	}
	standard := standardSpec.ForDynamicKey("ratelimit:chat:standard:", "chat:standard", broadcasterID)
	return w.takeOrdered(ctx, standard, shared)
}

// takeGeneralHelix consumes one token from the Helix budget backing the
// message's token identity: the general app partition, the bot user budget,
// or the target broadcaster's own user budget.
func (w *Worker) takeGeneralHelix(ctx context.Context, payload outgress.Message) error {
	standard, shared := generalHelixRequests(payload)
	if w.lane == LaneStandard {
		return w.takeOrdered(ctx, standard, shared)
	}
	return w.take(ctx, shared)
}

// takeSystemHelix consumes one token for a system-lane Helix call. The
// reserved partition is tried first, so EventSub enroll jobs always keep
// their guaranteed floor no matter how busy chat/api traffic is. When the
// reserve is momentarily drained (an onboarding burst, back-to-back
// reconnects) the call spills over into the general app partition instead of
// blocking the channel's enrollment. The reverse never happens — chat/api
// traffic still cannot touch the reserve — and every spilled token is one the
// general partition would have spent anyway, so the fleet stays within the
// real 800/min Helix limit.
func (w *Worker) takeSystemHelix(ctx context.Context) error {
	err := w.take(ctx, helixSystemSpec.ForKey("ratelimit:helix:system"))
	if !errors.Is(err, errRateLimitShared) {
		return err
	}
	return w.take(ctx, helixGeneralSpec.ForKey("ratelimit:helix:app"))
}

// take consumes one token or returns an error that nacks the message, so the
// paced redelivery retries it once the bucket has refilled.
func (w *Worker) take(ctx context.Context, req ratelimit.Request) error {
	started := time.Now()
	defer recordStageDuration(ctx, "outgress.limiter_ms", started)
	allowed, err := w.limiter.Allow(ctx, req)
	if err != nil {
		return err
	}
	if !allowed {
		return errRateLimitShared
	}
	return nil
}

func (w *Worker) takeOrdered(ctx context.Context, first, shared ratelimit.Request) error {
	started := time.Now()
	defer recordStageDuration(ctx, "outgress.limiter_ms", started)
	denied, err := w.limiter.AllowOrdered(ctx, first, shared)
	if err != nil {
		return err
	}
	switch denied {
	case 0:
		return nil
	case 1:
		return errRateLimitFirst
	default:
		return errRateLimitShared
	}
}
