package worker

import (
	"context"
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
	chatModSpec           = ratelimit.NewSpec(chatModCapacity, chatModCapacity/chatWindow)
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

// takeChat pays the per-broadcaster chat bucket, at mod capacity when the bot
// moderates the channel. This bucket IS the real Twitch per-channel chat limit
// (20/30s, or 100/30s when the bot moderates), keyed per broadcaster, so it is
// not a pool the premium and standard lanes contend over: a standard channel
// spending its own Twitch budget takes nothing a premium channel could have
// used. The lane therefore does not restrict this bucket. Premium priority for
// chat is enforced upstream at processing time, by the sesame consumer's
// reserved routine share (premium commands get handled first under load), not by
// throttling a standard channel's own send rate. The standard-lane half-reserve
// stays only on the genuinely shared Helix app budget (see takeGeneralHelix).
func (w *Worker) takeChat(ctx context.Context, broadcasterID string, isMod bool) error {
	spec := chatSpec
	if isMod {
		spec = chatModSpec
	}
	return w.take(ctx, spec.ForDynamicKey("ratelimit:chat:", "chat", broadcasterID))
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

// takeSystemHelix consumes one token from the reserved system partition.
// Only the system lane pays here, so dashboard EventSub jobs always have
// their reserved capacity and can never spend the general api budget.
func (w *Worker) takeSystemHelix(ctx context.Context) error {
	return w.take(ctx, helixSystemSpec.ForKey("ratelimit:helix:system"))
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
