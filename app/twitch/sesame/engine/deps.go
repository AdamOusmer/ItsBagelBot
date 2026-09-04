// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package engine is sesame's runtime: it takes the immutable module.Module
// values a Builder produced, indexes them in a Registry, and runs the
// interested ones for each message in the consumer's own goroutine. It owns the
// per-message orchestration (decode, command dispatch, event handlers, publish)
// and the shared command gate (permission, live-only, cooldown). The behavior
// lives in the modules (app/twitch/sesame/modules); the engine only wires and runs
// them.
//
// The command router is not a module here (as it was in the worker): dispatch is
// an engine stage that reads the registry's command index directly, which
// removes the worker's CommandRouter.Bind init-order footgun.
package engine

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

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
	QuoteSearch(ctx context.Context, broadcasterID uint64, term string) (modulesrpc.Quote, bool, error)
	QuoteEdit(ctx context.Context, broadcasterID, number uint64, text string) (modulesrpc.Quote, bool, error)
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
	Special  *SpecialSet
	Pub      bus.Publisher
	Commands CommandManager
	Gossip   GossipCaller
	// CustomFetch resolves {urlfetch:<name>} response tokens through gossip's
	// custom.fetch endpoint (the same NATS connection viewed through one more
	// narrow interface). nil leaves every such token visible, like any other
	// unresolved token.
	CustomFetch UrlFetchCaller
	Followage   FollowageLookup
	AccountAge  AccountAgeLookup
	Uptime      UptimeLookup
	Log         *zap.Logger
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
	// SongQueue is the per-broadcaster spotify song-request queue behind the
	// songqueue module; requests resolve through Deps.Gossip's spotify
	// provider and retract authorization keys on chatter ids. nil leaves the
	// module's commands inert.
	SongQueue SongQueueStore
	// Raffle is the per-broadcaster raffle behind the raffle module. nil
	// leaves the module's commands inert. Its deadline-key expiry auto-close
	// rides the same keyspace notifications as Timers and LoyaltyTick.
	Raffle RaffleStore
	// Duel is the per-broadcaster wager duel (pot and challenge flavors)
	// behind the duel module; its escrow rides Deps.Loyalty through a
	// DuelWallet. nil leaves the module's commands inert.
	Duel DuelStore
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
	// Stats sinks the bot-wide lifetime totals the pipeline keeps (bot-scope
	// counters under broadcaster 0); LoyaltyReporter is the default. nil starts
	// no flusher, so tests leak no goroutine and publish nothing.
	Stats CounterBumper
	// PublicBaseURL is the origin of the public console pages; the !cmd module
	// builds the channel command-page link from it. Empty falls back to the
	// production dashboard origin.
	PublicBaseURL string
	// Personality is the small state behind the built-in personality module
	// (fact rotation, feed counter, stream mood); ValkeyPersonality is the
	// default. nil degrades gracefully: facts go random, the feed count is
	// omitted and the mood re-rolls per message.
	Personality PersonalityStore
	// EmotePlay is the per-channel emote pyramid + streak tracker behind the
	// emoteplay module; ValkeyEmotePlay is the default. nil leaves the module
	// silent (it never emits without a store).
	EmotePlay EmotePlayStore
	// Dedup guards the non-idempotent effect sites against a redelivered or
	// schedule-retried event applying them twice. nil (the kill switch) fails
	// open everywhere: effects run, nothing is deduped.
	Dedup *EventDedup
	// Seq serializes per-broadcaster the background work the stream-lifecycle
	// handlers enqueue (live key writes, greet resets, timer/loyalty tick
	// arm-disarm, gossip session snapshots): tasks run in arrival order, one
	// fully complete before the next starts, so an offline handler's disarm can
	// no longer race the online handler's arm on this replica (#561). It is the
	// intra-replica half of the fix — the versioned LiveStore writes are the
	// cross-replica half. nil leaves every handler plain fire-and-forget.
	Seq *Sequencer
	// Nuke is the phrase-targeted mass-moderation service behind !nuke. When
	// set, the pipeline feeds each chat line into its recentStore (the sweep
	// memory — ValkeyRecent in production, centralized because the replica
	// pool shares one durable consumer) and binds its Shield Mode escalation
	// decision; the moderation module reads the same handle to run sweeps.
	// nil disables both: nothing is recorded and !nuke is inert.
	Nuke *Nuke
}

// FeedCounts is one feeding's fleet-wide readout: how often the bagel has been
// fed today (valkey, TTL window) and ever (the modules service's permanent
// row). One bagel, fed by every channel. The per-channel breakdown behind it
// is read separately, by the leaderboard commands.
type FeedCounts struct {
	Today uint64
	Total uint64
}

// FeedBoardEntry is one channel's place on the feed leaderboard: the channel,
// the display name it carried at its last feeding, and its lifetime count.
type FeedBoardEntry struct {
	BroadcasterID uint64
	Name          string
	Count         uint64
}

// FeedBoard is a leaderboard readout: the top channels, how many channels are
// ranked, and the asking channel's own standing (which may sit below the
// returned entries). Channel and Rank are 0 for a channel that never fed.
type FeedBoard struct {
	Entries []FeedBoardEntry
	Ranked  uint64
	Channel uint64
	Rank    uint64
}

// PersonalityStore is the state the personality module leans on. Fact and mood
// are best-effort: the module treats an error like a nil store and falls back
// to stateless randomness. Feed is not: without counts there is no feed line,
// so an error there silences the reaction.
type PersonalityStore interface {
	// FactCursor bumps and returns a per-channel monotonic counter; the module
	// indexes the fact list with it modulo the list length.
	FactCursor(ctx context.Context, broadcasterID uint64) (int64, error)
	// Feed records one feeding by this channel and returns the fleet-wide
	// counts. name is the channel's display name, stored so leaderboard lines
	// can name it.
	Feed(ctx context.Context, broadcasterID uint64, name string) (FeedCounts, error)
	// FeedBoard reads the leaderboard without feeding anything: limit caps the
	// entries (negative asks for the standing alone) and broadcasterID asks for
	// that channel's own count and rank.
	FeedBoard(ctx context.Context, broadcasterID uint64, limit int) (FeedBoard, error)
	// Mood returns the stream's mood, seeding it with candidate when unset.
	Mood(ctx context.Context, broadcasterID uint64, candidate string) (string, error)
}

// FeedTotals is what the permanent store returns for one recorded feeding: the
// fleet-wide lifetime total plus this channel's own lifetime count and rank.
type FeedTotals struct {
	Total   uint64
	Channel uint64
	Rank    uint64
}

// FeedTotalPersister writes feedings to the permanent counters and reads the
// leaderboard back; PersonalityRPC (the modules service) is the default.
type FeedTotalPersister interface {
	// FeedBump records one feeding on the fleet-wide counter and, when the
	// feeding names a channel, that channel's row too.
	FeedBump(ctx context.Context, broadcasterID uint64, name string) (FeedTotals, error)
	// FeedBoard reads the leaderboard and the named channel's standing.
	FeedBoard(ctx context.Context, broadcasterID uint64, limit int) (FeedBoard, error)
}

// EmotePlayStore advances a channel's emote chains by one candidate line. See
// ValkeyEmotePlay (emoteplay_valkey.go) for the transition rules and the
// race-safety reasoning; callers only feed lines and read back milestones.
type EmotePlayStore interface {
	Bump(ctx context.Context, u EmotePlayUpdate) (EmotePlayResult, error)
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
// The writes are versioned (#561): version is the event's ordering claim (the
// envelope's EventVersion, unix millis). A write whose version is older than
// what is already applied is skipped rather than overwriting — a rapid
// stream.online/offline pair processed by different consumer goroutines must
// not let the online land last and resurrect the key. applied reports whether
// this call won; callers skip their follow-up effects when it did not, so a
// superseded online never re-arms timers an offline just disarmed.
type LiveStore interface {
	IsLiveChecker
	SetLive(ctx context.Context, broadcasterID uint64, version int64) (applied bool, err error)
	ClearLive(ctx context.Context, broadcasterID uint64, version int64) (applied bool, err error)
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

// Viewer is the bumping chatter's identity as the source event knew it: the
// id keys viewer-scoped buckets; login/name ride along (possibly empty) so
// the loyalty service can store a readable display identity next to the
// bucket and refresh it whenever it changes.
type Viewer struct {
	ID    uint64
	Login string
	Name  string
}

// CounterBump is one counter increment request: which broadcaster's counter
// (name), against whose identity (viewer — zero rides the shared value),
// keyed by which source (the command trigger or reward title; only the scopes
// that bucket per source use it), and by how much.
type CounterBump struct {
	BroadcasterID uint64
	Name          string
	Viewer        Viewer
	Command       string
	Delta         int64
}

// LoyaltyStore is the loyalty surface modules and the pipeline depend on:
// point accrual (fire-and-forget through the worker-side reporter), counter
// bumps/reads over the Valkey live view, cached balance peeks and the
// authoritative counter management verbs. ValkeyLoyaltyStore is the default.
type LoyaltyStore interface {
	// Earn records one viewer's point/watch accrual; batched and loss-tolerant.
	Earn(broadcasterID, viewerID uint64, login, name string, points int64, watchSeconds uint64)
	// CounterBump increments a counter and returns the new value. Which fields
	// of the request matter is decided by the counter's own scope: viewer
	// identity keys the per-viewer buckets, command names the per-source
	// bucket, channel scope ignores both.
	CounterBump(ctx context.Context, b CounterBump) (int64, error)
	// CounterPeek reads a counter without bumping it; found=false means it
	// does not exist.
	CounterPeek(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string) (loyaltyrpc.Counter, bool, error)
	// BalanceGet returns one viewer's standing (zero-valued when unseen).
	BalanceGet(ctx context.Context, broadcasterID, viewerID uint64) (loyaltyrpc.Balance, error)
	// BalanceAdjust writes a viewer's points by login (mod grants): absolute
	// sets, otherwise value is a delta. found=false = login never seen here.
	BalanceAdjust(ctx context.Context, broadcasterID uint64, viewerLogin string, value int64, absolute bool) (loyaltyrpc.Balance, bool, error)
	// BalanceSpend conditionally debits points by login (the wager games'
	// escrow): the debit lands only when the viewer holds at least amount.
	// found=false = login never seen here; spent=false with found=true =
	// insufficient points (bal carries what they hold).
	BalanceSpend(ctx context.Context, broadcasterID uint64, viewerLogin string, amount int64) (bal loyaltyrpc.Balance, found, spent bool, err error)
	// BalanceTransfer moves fromViewerID's own points to the target login
	// ("!points give"): an atomic guarded debit plus credit. found=false =
	// target login never seen here; moved=false with found=true = the sender
	// could not cover it (bal carries their standing).
	BalanceTransfer(ctx context.Context, broadcasterID, fromViewerID uint64, targetLogin string, amount int64) (bal loyaltyrpc.Balance, found, moved bool, err error)
	// Top returns the channel's points leaderboard, highest first.
	Top(ctx context.Context, broadcasterID uint64, limit int) ([]loyaltyrpc.Balance, error)
	// CounterCreate/CounterSet/CounterDelete/CounterList are the authoritative
	// management verbs behind !counter (and the future dashboard).
	CounterCreate(ctx context.Context, broadcasterID uint64, name, scope string) (loyaltyrpc.Counter, error)
	CounterSet(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string, value int64) (bool, error)
	CounterDelete(ctx context.Context, broadcasterID uint64, name string) error
	CounterList(ctx context.Context, broadcasterID uint64) ([]loyaltyrpc.Counter, error)
}

// CounterBumper is the narrow slice of LoyaltyReporter the pipeline's bot-wide
// stats flusher needs: one batched, loss-tolerant bot-scope counter delta.
type CounterBumper interface {
	BumpBot(name string, delta int64)
	BumpChannel(broadcasterID uint64, name string, delta int64)
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
