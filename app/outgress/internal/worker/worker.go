// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package worker drains one outgress lane: it enforces the channel registry,
// the Twitch rate limits, and the premium reservation, then executes the
// Helix request. Handlers nack on anything retryable and rely on the lane
// subscriber's paced redelivery, so a rate-limited or failing message waits
// out its budget instead of spinning.
package worker

import (
	"context"
	"time"

	"ItsBagelBot/app/outgress/internal/action"
	"ItsBagelBot/app/outgress/internal/channels"
	"ItsBagelBot/app/outgress/internal/conduit"
	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/app/outgress/internal/youtube"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

// nodeRegion and nodeName label every transaction so Twitch external-segment
// duration can be faceted by node in New Relic. They are process-wide (one pod
// is one node) and set once at startup via SetNodeIdentity; the empty default
// is harmless when the agent is not configured.
var (
	nodeRegion string
	nodeName   string
)

// SetNodeIdentity records the pod's region and host for transaction labeling.
// Call once at startup before consuming.
func SetNodeIdentity(region, host string) {
	nodeRegion = region
	nodeName = host
}

// Lane identifies which queue a worker drains; it selects the rate-limit
// buckets the worker pays into.
type Lane int

const (
	LanePremium Lane = iota
	LaneStandard
	LaneSystem
)

type expectedNackError string

func (e expectedNackError) Error() string      { return string(e) }
func (e expectedNackError) ExpectedNack() bool { return true }

// Expected backpressure must nack without becoming one warning and one noticed
// error per attempt. pkg/bus recognizes ExpectedNack structurally.
const (
	ErrPaused          expectedNackError = "outgress is paused"
	errRateLimitFirst  expectedNackError = "rate limit exceeded on reserved bucket"
	errRateLimitShared expectedNackError = "rate limit exceeded on shared bucket"
)

type Worker struct {
	log      *zap.Logger
	limiter  ratelimit.Manager
	registry *channels.Registry
	twitch   *twitch.Client
	botID    string
	owner    string // pod identity for the enroll lock (os.Hostname)
	conduit  *conduit.Resolver
	lane     Lane
	batch    BatchStore
	// actions is the immutable per-type dispatch registry built once in New
	// (see buildActions); every lane message resolves through one lock-free
	// lookup in it.
	actions action.Registry
	// userIDs caches login->id resolutions (shoutout targets) so a repeated
	// /shoutout to the same channel does not re-hit Helix Get Users each time.
	// Wiring injects one instance shared by all three lane workers via
	// Config.UserIDs; it is a small, fleet-shared keyspace that is not
	// lane-specific, so a per-worker copy would only duplicate resident memory.
	userIDs *cache.Cache[string]
	// modVerifier resolves stale moderator state asynchronously so chat sends
	// never wait for a paginated Twitch lookup or OAuth refresh.
	modVerifier *ModVerifier
	// live writes the result of a Twitch live re-check back into the projection.
	// Only the system lane sets it (via SetLiveWriter); nil elsewhere.
	live *LiveWriter
	// reauth tells a streamer their Twitch grant died (dashboard bell + the
	// go-live chat beacon copy). Wiring attaches one shared instance to all
	// three lanes: the system lane drives the beacon and the authz consumers,
	// and the chat lanes raise the bell the moment a broadcaster-identity call
	// proves the grant dead. Nil in tests, where every call site degrades to a
	// no-op.
	reauth *ReauthNotifier
	// grants is the narrow registry slice the grant marker uses. It points at
	// the same *channels.Registry as the field above; the separate, smaller
	// interface exists so the marker's transition logic is testable without
	// Valkey. Nil when no registry is configured, which disables the marker.
	grants grantRegistry
	// clipVerify bounds the in-flight background clip publication checks (see
	// scheduleClipVerify). A full channel skips the check instead of queueing:
	// the clip and its reply are unaffected, only the failure notice is lost.
	clipVerify chan struct{}
	// tokenWarm bounds the in-flight background broadcaster-token pre-warms
	// (see scheduleTokenWarm/SubscribeTokenWarm in tokenwarm.go). A full
	// channel skips the warm instead of queueing: the cache stays cold and the
	// next real "as":"broadcaster" send pays the lazy cold mint, exactly the
	// pre-fix status quo, so dropping under saturation costs nothing but the
	// optimization.
	tokenWarm chan struct{}
	// guards is the direct central limiter backing the guard buckets (see
	// guards.go for why these deliberately do NOT ride the lease manager).
	// Nil (unset) disables guarding; every allow helper fails open.
	guards ratelimit.Manager
	// youtube is the Data API client slice the YouTube handlers fire through,
	// attached with its budget and chat directory via SetYouTube. Nil until
	// then: the four youtube_* types are absent from the action registry, so
	// no message can reach the handlers.
	youtube  youtubeAPI
	ytBudget ytBudget
	ytChats  *youtube.ChatDirectory
	// discord is the REST client slice processDiscordChat fires through,
	// attached via SetDiscord. Same nil-until-attach contract as youtube.
	discord discordAPI
}

// Config wires one lane worker's collaborators.
type Config struct {
	Log      *zap.Logger
	Limiter  ratelimit.Manager
	Registry *channels.Registry
	Twitch   *twitch.Client
	BotID    string
	Owner    string // pod identity for the enroll lock (os.Hostname)
	Conduit  *conduit.Resolver
	Lane     Lane
	Batch    BatchStore
	// UserIDs is the shared login->id cache. Wiring builds one via NewUserIDCache
	// and passes it to every lane worker so they share a single resident copy. A
	// nil value makes New fall back to a private cache, which keeps a standalone
	// worker (tests) usable but forfeits the sharing.
	UserIDs *cache.Cache[string]
}

func New(cfg Config) *Worker {
	userIDs := cfg.UserIDs
	if userIDs == nil {
		userIDs = NewUserIDCache()
	}
	// Assign through a nil check rather than directly: a nil *channels.Registry
	// stored in an interface field is a non-nil interface, which would defeat
	// every nil guard on the grant marker path.
	var grants grantRegistry
	if cfg.Registry != nil {
		grants = cfg.Registry
	}
	w := &Worker{
		grants:     grants,
		log:        cfg.Log,
		limiter:    cfg.Limiter,
		registry:   cfg.Registry,
		twitch:     cfg.Twitch,
		botID:      cfg.BotID,
		owner:      cfg.Owner,
		conduit:    cfg.Conduit,
		lane:       cfg.Lane,
		batch:      cfg.Batch,
		userIDs:    userIDs,
		clipVerify: make(chan struct{}, clipVerifySlots),
		tokenWarm:  make(chan struct{}, tokenWarmSlots),
	}
	// Handlers capture w by method value, so late-attached collaborators
	// (SetModVerifier, SetReauthNotifier, SetLiveWriter) are still seen.
	w.actions = w.buildActions()
	return w
}

// SetLiveWriter attaches the live re-check write-back, used by the system lane
// worker that handles stream_status jobs.
func (w *Worker) SetLiveWriter(lw *LiveWriter) { w.live = lw }

func (w *Worker) SetModVerifier(v *ModVerifier) { w.modVerifier = v }

// SetReauthNotifier attaches the streamer-facing reauth messaging. Every lane
// gets it: the system lane for the go-live beacon and the authorization
// lifecycle events, the chat lanes for the dashboard bell raised the moment a
// broadcaster-identity call proves the grant dead.
func (w *Worker) SetReauthNotifier(r *ReauthNotifier) { w.reauth = r }

// rebuildActions redeclares the immutable registry from the same builder
// buildActions uses, plus whatever a late-attached platform adds. The four
// YouTube types and the Discord type are only registered once their client is
// attached (SetYouTube/SetDiscord), so a bare worker drops them as unknown
// instead of panicking on a nil client mid-dispatch. The swap is a single
// assignment done at wiring time before any consumer starts, which keeps
// lookups lock-free without adding mutation discipline around a shared map.
func (w *Worker) rebuildActions(extend func(b *action.Builder)) {
	b := action.NewSet()
	w.declareActions(b)
	if extend != nil {
		extend(b)
	}
	w.actions = b.Build()
}

// SetYouTube attaches the Data API client, the fleet-shared daily-quota budget,
// and the lifecycle-learned chat directory, then registers the four youtube_*
// actions. Wiring calls it once per lane worker that drains YouTube subjects,
// passing the SAME collaborators to each so one budget and one directory serve
// every lane.
func (w *Worker) SetYouTube(client youtubeAPI, budget ytBudget, chats *youtube.ChatDirectory) {
	w.youtube, w.ytBudget, w.ytChats = client, budget, chats
	w.rebuildActions(func(b *action.Builder) {
		b.Action(outgress.TypeYouTubeChat).Internal().Run(w.processYouTubeChat)
		b.Action(outgress.TypeYouTubeDelete).Internal().Run(w.processYouTubeDelete)
		b.Action(outgress.TypeYouTubeBan).Internal().Run(w.processYouTubeBan)
		b.Action(outgress.TypeYouTubeTimeout).Internal().Run(w.processYouTubeTimeout)
	})
}

// SetDiscord attaches the Discord REST client and registers the discord_chat
// action. Unwired in main for now: no Discord deployment exists yet.
func (w *Worker) SetDiscord(client discordAPI) {
	w.discord = client
	w.rebuildActions(func(b *action.Builder) {
		b.Action(outgress.TypeDiscordChat).Internal().Run(w.processDiscordChat)
	})
}

// Login->id resolutions (shoutout targets) are a small, fleet-shared keyspace,
// so wiring builds one bounded cache and injects it into every lane worker
// instead of each holding a default-capacity copy. Capacity and TTL are kept
// explicit here.
const (
	UserIDCacheCapacity = 1024
	UserIDCacheTTL      = 10 * time.Minute
)

// NewUserIDCache builds the shared shoutout login->id cache. Wiring calls it
// once and passes the result to every lane worker via Config.UserIDs.
func NewUserIDCache() *cache.Cache[string] {
	return cache.New[string](UserIDCacheCapacity, UserIDCacheTTL)
}

func recordStageDuration(ctx context.Context, attribute string, started time.Time) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute(attribute, float64(time.Since(started).Microseconds())/1000)
	}
}

// noticeError forwards err to the transaction's error trace when the request
// runs under a New Relic transaction.
func noticeError(ctx context.Context, err error) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.NoticeError(err)
	}
}

// botIdentity resolves the bot identity a job acts as (chat sender or acting
// moderator): the configured bot id wins, else the message's sender id.
// ok=false means neither is set - there is nobody to act as, so the caller
// must drop the job (already logged here, ack).
func (w *Worker) botIdentity(action string, payload *outgress.Message) (string, bool) {
	// Prefer the configured identity over producer-controlled wire data: a
	// compromised producer must not be able to re-persona the bot by setting
	// sender_id on the wire. SenderID stays the fallback for deployments that
	// run without TWITCH_BOT_USER_ID.
	id := w.botID
	if id == "" {
		id = payload.SenderID
	}
	if id == "" {
		w.log.Error("dropping "+action+": no bot identity configured",
			zap.String("broadcaster_id", payload.BroadcasterID))
		return "", false
	}
	return id, true
}

// modStatus is deliberately non-blocking: use the last known value and let the
// shared verifier refresh stale state away from the chat handler.
func (w *Worker) modStatus(_ context.Context, payload *outgress.Message, ch manage.Channel, found bool) bool {
	if w.modVerifier == nil {
		return found && ch.IsMod
	}
	return w.modVerifier.Status(ch, found, payload.BroadcasterID, payload.SenderID)
}

func (w *Worker) scheduleModStatus(broadcasterID, senderID string) {
	if w.modVerifier != nil {
		w.modVerifier.Schedule(broadcasterID, senderID)
	}
}
