// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"go.uber.org/zap"
)

// songqueueModuleName is the ModuleView key; the console MODULE_CATALOG entry
// uses the same id.
const songqueueModuleName = "songqueue"

// songqueueListLen is how many up-next entries the bare !sr view shows;
// anything past it is summarized as a "+N more" tail so a long line never
// floods chat.
const songqueueListLen = 3

// defaultSongQueueDepth is the pending-line cap when the broadcaster has not
// configured one; hardSongQueueDepth is the ceiling on what they CAN set, so
// one misconfigured dashboard value cannot promise an unplayable marathon.
const (
	defaultSongQueueDepth = 100
	hardSongQueueDepth    = 1000
)

// srAddCooldown throttles !sr per chatter in the engine gate: every add costs
// a live Spotify lookup through gossip, so an uncooled spam loop spends the
// broadcaster's token quota for junk entries.
const srAddCooldown = 5 * time.Second

// currentCooldown paces !current. Every miss is a live read of the
// broadcaster's Spotify player against THEIR token allowance, and gossip
// caches the answer for a few seconds, so a shorter cooldown would only spend
// chat's patience re-reading a cached line.
const currentCooldown = 5 * time.Second

// songqueueConfig holds the broadcaster's overrides. MaxDepth caps the
// pending line; the message fields are dashboard-editable templates whose
// empty value falls back to the localized default next to each.
type songqueueConfig struct {
	MaxDepth       int    `json:"maxDepth"`
	AddMessage     string `json:"addMessage"`     // i18n songqueue.add.ok   {user} {title} {artist} {pos}
	PlayingMessage string `json:"playingMessage"` // i18n songqueue.playing  {title} {artist} {req}
	RetractMessage string `json:"retractMessage"` // i18n songqueue.retract.ok {user} {title}
	// CurrentMessage overrides both !current lines. {req} only expands when
	// the playing track is the one at the head of the queue: a track the
	// broadcaster started themselves has no requester to credit.
	CurrentMessage string `json:"currentMessage"` // i18n songqueue.current.ok {title} {artist} {url} {req}
	// Sr and Redeem are the two request-path switches the dashboard writes.
	// Pointers on purpose: a blob written before the switches shipped has
	// neither key, and nil means "pre-switch behaviour": chat open, no live
	// gate, so enabling the module never stopped working under a viewer.
	Sr     *songqueueSr     `json:"sr"`
	Redeem *songqueueRedeem `json:"redeem"`
}

// songqueueSr is the chat (!sr) request path. Enabled gates adds; Perm is a
// module.ParsePerm string; AllowOffline opts out of the live-only gate the
// same way govee's allowOffline does (default false = live-only).
type songqueueSr struct {
	Enabled      bool   `json:"enabled"`
	Perm         string `json:"perm"`
	AllowOffline bool   `json:"allowOffline"`
}

// songqueueRedeem is the channel-points request path. RewardID binds the
// Twitch custom reward the redemption handler answers to; OnRedeem is what to
// do with the redemption after a successful queue (fulfill/cancel/leave);
// AllowOffline mirrors the chat path's live gate polarity.
type songqueueRedeem struct {
	Enabled      bool   `json:"enabled"`
	RewardID     string `json:"rewardId"`
	OnRedeem     string `json:"onRedeem"`
	ReplyMessage string `json:"replyMessage"`
	AllowOffline bool   `json:"allowOffline"`
}

// SongQueue owns the viewer song-request queue, resolved against the
// broadcaster's connected Spotify account through the gossip spotify
// provider. It is opt-in like the player queue; on channels that never enable
// it the !sr spelling falls through to custom commands untouched.
//
//	!current                              → what Spotify is playing RIGHT NOW
//	!sr <song|artist - song|spotify link> → resolve + queue (one per viewer)
//	!sr                                   → now playing + the next few
//	!sr remove / retract / cancel         → take back YOUR queued request
//	!sr remove <position>                 → mod: remove anyone's entry
//	!sr next                              → mod: mark played, promote next
//	!sr clear                             → mod: empty everything
//
// next/clear are verbs only as bare words: "next episode by dr. dre" is a
// request for a song, not a skip.
//
// Retraction keys on the chatter's Twitch user id captured at request time,
// never the display name, and only ever touches that viewer's own pending
// entry: "the one they asked" and nothing else. The currently-playing track
// is out of reach on purpose: it already played.
func SongQueue(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(songqueueModuleName, module.KindOptIn)
	m.Command("sr").Everyone().Cooldown(srAddCooldown).
		Aliases("songrequest", "songreq").
		Run(songQueueDispatch(d, log))

	// Standalone spellings for the two things viewers ask for by name. They are
	// separate commands rather than aliases of !sr because an alias arrives as a
	// bare invocation, which would make every one of them mean "view": !skip
	// has to advance the queue.
	//
	// NOT !queue: the viewer queue module (queue.go) already owns that name, and
	// command precedence is registration order in All(), so claiming it here
	// would shadow a different feature on any channel running both. !song and
	// !current say what this one is about anyway.
	//
	// No cooldown: a read and a mod action, neither spends the Spotify lookup an
	// add does.
	m.Command("song").Everyone().Cooldown(currentCooldown).
		Aliases("current", "nowplaying", "np").
		Run(songQueueView(d, log))

	// !skip carries its moderator gate on the registration, where the !sr next
	// sub-verb has to enforce it by hand: a bare word after !sr could also be
	// a song title, so that path cannot lean on the command's own permission.
	m.Command("skip").Mod().
		Aliases("next").
		Run(songQueueSkip(d, log))

	// !clear and !remove are the standalone spellings of the two !sr verbs
	// chat reaches for by name. Without them the trigger falls through to a
	// custom command, which replies without ever touching the queue: the
	// failure mode that reads as "the bot ignored me". !remove is Everyone
	// because it retracts the caller's OWN request; the positional form
	// inside actRemove is what carries the moderator check.
	m.Command("clear").Mod().
		Run(songQueueClear(d, log))
	m.Command("remove").Everyone().
		Run(songQueueRemove(d, log))

	// Channel-points path: the redemption of the bound reward queues a track.
	// Registered unconditionally, because the handler answers only to the reward id
	// in this channel's config and no-ops for every other redemption.
	m.On(redemptionAddType, songqueueRedemption(d, log))
	return m.Build()
}

// songQueueCmd bundles the per-invocation state every handler shares, built
// once by newSongQueueCmd, the same shape as queueCmd.
type songQueueCmd struct {
	store    engine.SongQueueStore
	gossip   engine.GossipCaller
	live     engine.IsLiveChecker
	c        *module.Context
	cfg      songqueueConfig
	log      *zap.Logger
	maxDepth int
}

func newSongQueueCmd(d engine.Deps, c *module.Context, log *zap.Logger) (qc songQueueCmd, ok bool) {
	if d.SongQueue == nil {
		return songQueueCmd{}, false
	}
	qc = songQueueCmd{store: d.SongQueue, gossip: d.Gossip, live: d.Live, c: c, log: log}
	_ = c.Decode(&qc.cfg)
	qc.maxDepth = qc.cfg.MaxDepth
	if qc.maxDepth <= 0 {
		qc.maxDepth = defaultSongQueueDepth
	}
	if qc.maxDepth > hardSongQueueDepth {
		qc.maxDepth = hardSongQueueDepth
	}
	return qc, true
}

// songQueueAction handles one recognized !sr leading word: args is the whole
// trimmed input (a request fallback needs it verbatim), rest is what followed
// the verb.
type songQueueAction func(qc *songQueueCmd, ctx context.Context, args, rest string, emit module.Emit) error

// songQueueActions routes a recognized leading verb to its handler. Unknown
// first words are queries, not subcommands.
var songQueueActions = map[string]songQueueAction{
	"retract": (*songQueueCmd).actRetract,
	"cancel":  (*songQueueCmd).actRetract,
	"remove":  (*songQueueCmd).actRemove,
	"next":    (*songQueueCmd).actNext,
	"skip":    (*songQueueCmd).actNext,
	"clear":   (*songQueueCmd).actClear,
}

// songQueueView backs !song / !current / !nowplaying / !np.
func songQueueView(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		qc, ok := newSongQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		return qc.current(ctx, emit)
	}
}

// current answers !current from the broadcaster's LIVE Spotify player rather
// than from the queue. Chat asks "what is this song" about whatever is
// actually audible, and on most channels that is the broadcaster's own
// playlist: a queue read answers "nothing queued" while a song is plainly
// playing, which reads as the bot being broken.
//
// An idle player (paused, private session, nothing loaded) degrades to the
// queue view: with nothing playing, what is waiting is the only thing left
// worth saying. Bare !sr keeps the queue view unconditionally, that command
// is about the queue.
func (qc songQueueCmd) current(ctx context.Context, emit module.Emit) error {
	track, failure := qc.livePlayer(ctx)
	if failure != "" {
		qc.emitChat(emit, failure)
		return nil
	}
	if track == nil {
		return qc.view(ctx, emit)
	}
	kv := []string{
		"title", track.Name,
		"artist", strings.Join(track.Artists, ", "),
		"url", track.URL,
	}
	key := "songqueue.current.ok"
	if req := qc.requesterOf(ctx, track.ID); req != "" {
		kv = append(kv, "req", req)
		key = "songqueue.current.req"
	}
	qc.reply(emit, qc.cfg.CurrentMessage, key, kv...)
	return nil
}

// livePlayer reads the broadcaster's currently-playing track, or the
// user-facing line explaining why it could not. A nil track with no failure
// means "nothing is playing", which is an answer rather than an error: a
// paused player, a private listening session and an idle account are
// indistinguishable here and all mean the same thing to chat.
func (qc songQueueCmd) livePlayer(ctx context.Context) (*gossiprpc.SpotifyTrack, string) {
	if qc.gossip == nil {
		return nil, ""
	}
	var reply gossiprpc.SpotifyNowPlayingReply
	err := qc.gossip.Call(ctx,
		engine.GossipRoute{Provider: "spotify", Endpoint: "nowplaying"},
		gossiprpc.Request{ChannelID: strconv.FormatUint(qc.c.BroadcasterID, 10)}, &reply)
	if reply.Error != "" {
		// Chat-safe already (no Spotify app set up for this channel, the
		// connection needs redoing): surfaced verbatim, like the search path.
		return nil, reply.Error
	}
	if err != nil {
		qc.log.Warn("songqueue: nowplaying rpc failed", qc.bid(), zap.Error(err))
		return nil, i18n.T(qc.c.Locale, "songqueue.err.upstream")
	}
	if !reply.IsPlaying {
		return nil, ""
	}
	return reply.Track, ""
}

// requesterOf credits the viewer who asked for the playing track, and only
// them: the credit is dropped unless the live track IS the queue head, so a
// song the broadcaster started themselves never gets attributed to whoever
// happens to be next in line. A snapshot failure costs the credit, not the
// answer.
func (qc songQueueCmd) requesterOf(ctx context.Context, trackID string) string {
	snap, err := qc.store.Snapshot(ctx, qc.c.BroadcasterID, songqueueListLen)
	if err != nil {
		return ""
	}
	if snap.Current == nil {
		return ""
	}
	if snap.Current.TrackID != trackID {
		return ""
	}
	return snap.Current.RequesterName
}

// songQueueSkip backs !skip / !next: the same advance !sr next performs. The moderator
// gate rides on the command registration here rather than the hand-rolled check
// bareModVerb needs, because there is no query for it to be confused with.
func songQueueSkip(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		qc, ok := newSongQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		return qc.nextTrack(ctx, emit)
	}
}

// songQueueClear backs standalone !clear (mod grant on registration).
func songQueueClear(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		qc, ok := newSongQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		return qc.clearAll(ctx, emit)
	}
}

// songQueueRemove backs standalone !remove: retract your own request, or drop
// a position when a mod supplies a number, the same rules as !sr remove, so
// both spellings answer identically.
func songQueueRemove(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		qc, ok := newSongQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		return qc.actRemove(ctx, args, strings.TrimSpace(args), emit)
	}
}

func songQueueDispatch(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		qc, ok := newSongQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		args = strings.TrimSpace(args)
		if args == "" {
			return qc.view(ctx, emit)
		}
		sub, rest := splitFirst(args)
		act := songQueueActions[strings.ToLower(sub)]
		if act == nil {
			return qc.request(ctx, args, emit)
		}
		return act(&qc, ctx, args, rest, emit)
	}
}

func (qc songQueueCmd) actRetract(ctx context.Context, _, _ string, emit module.Emit) error {
	return qc.retract(ctx, emit)
}

// actRemove treats a number as aiming at someone else's position (mod-only);
// anything else (including a non-mod guessing at numbers) retracts the
// chatter's own request, which is always safe.
func (qc songQueueCmd) actRemove(ctx context.Context, _ string, rest string, emit module.Emit) error {
	if n := parsePosition(rest); n > 0 && qc.c.Chatter().Allows(module.RoleModerator) {
		return qc.removeAt(ctx, n, emit)
	}
	return qc.retract(ctx, emit)
}

func (qc songQueueCmd) actNext(ctx context.Context, args, rest string, emit module.Emit) error {
	return qc.bareModVerb(ctx, args, rest, emit, qc.nextTrack)
}

func (qc songQueueCmd) actClear(ctx context.Context, args, rest string, emit module.Emit) error {
	return qc.bareModVerb(ctx, args, rest, emit, qc.clearAll)
}

// bareModVerb gates the bare-word mod verbs next/clear: with anything
// trailing, the whole input reads back as a query, so song titles starting
// with these words still resolve instead of being eaten as a mod command;
// a bare word from a non-mod is silently ignored.
func (qc songQueueCmd) bareModVerb(ctx context.Context, args, rest string, emit module.Emit, mod func(context.Context, module.Emit) error) error {
	if rest != "" {
		return qc.request(ctx, args, emit)
	}
	if !qc.c.Chatter().Allows(module.RoleModerator) {
		return nil
	}
	return mod(ctx, emit)
}

// request resolves free text / links to exactly one track via the gossip
// spotify provider and queues it for the asking viewer.
func (qc songQueueCmd) request(ctx context.Context, query string, emit module.Emit) error {
	if !qc.canRequest() {
		return nil
	}
	// A refusal SAYS so. Silence here is what makes a closed switch look like
	// a broken bot: the viewer typed a command the catalog advertises, the
	// module is on, and nothing came back. The engine's per-chatter cooldown
	// on !sr is what keeps the refusal from being spammable.
	if key := qc.srRefusal(); key != "" {
		qc.reply(emit, "", key)
		return nil
	}
	if !qc.chatLiveOK(ctx) {
		qc.reply(emit, "", "songqueue.sr.offline")
		return nil
	}
	track, failure := qc.resolveTrack(ctx, query)
	if failure != "" {
		qc.emitChat(emit, failure)
		return nil
	}
	pos, err := qc.store.Add(ctx, qc.c.BroadcasterID, qc.entry(*track), qc.maxDepth)
	return qc.reportAdd(emit, *track, pos, err)
}

// srRefusal names the line to answer a chat add with, or "" when the path is
// open. A missing sr block (a blob from before the path switches shipped)
// stays open so channels that only ever flipped the master toggle keep
// working; an explicit false closes adds while the view and the mod verbs
// still run.
func (qc songQueueCmd) srRefusal() string {
	if qc.cfg.Sr == nil {
		return ""
	}
	if !qc.cfg.Sr.Enabled {
		return "songqueue.sr.off"
	}
	if !qc.c.Chatter().Allows(module.ParsePerm(qc.cfg.Sr.Perm)) {
		return "songqueue.sr.perm"
	}
	return ""
}

// chatLiveOK is the live gate for chat adds, shaped exactly like govee's:
// legacy blobs with no sr block skip it, and once the dashboard has written
// sr, live-only is the default with AllowOffline as the opt-out.
func (qc songQueueCmd) chatLiveOK(ctx context.Context) bool {
	if qc.cfg.Sr == nil {
		return true
	}
	return qc.livePermits(ctx, qc.cfg.Sr.AllowOffline)
}

// livePermits mirrors goveeLivePermits: live-only by default, AllowOffline
// opts out, and a live-check error fails CLOSED: a queue that fills while the
// stream is down is worse than an add that did not land. It hangs off the
// command struct because both request paths already hold one, and the checker,
// the logger and the broadcaster id all come from it.
func (qc songQueueCmd) livePermits(ctx context.Context, allowOffline bool) bool {
	if allowOffline {
		return true
	}
	if qc.live == nil {
		return true
	}
	ok, err := qc.live.IsLive(ctx, qc.c.BroadcasterID)
	if err != nil {
		qc.log.Warn("songqueue: live check failed, denying", qc.bid(), zap.Error(err))
		return false
	}
	return ok
}

// canRequest gates adds on a gossip connection and the broadcaster's twitch
// user id being on file; without either there is nothing to resolve against.
func (qc songQueueCmd) canRequest() bool {
	return qc.gossip != nil && qc.c.Env.ChatterUserID != ""
}

// reportAdd answers the queue outcome: duplicate and full get their localized
// lines, an infrastructure error is logged here and bubbles to the engine,
// success announces through the broadcaster's add template.
func (qc songQueueCmd) reportAdd(emit module.Emit, track gossiprpc.SpotifyTrack, pos int, err error) error {
	switch {
	case errors.Is(err, engine.ErrSongAlreadyQueued):
		qc.reply(emit, "", "songqueue.add.already")
	case errors.Is(err, engine.ErrSongQueueFull):
		qc.reply(emit, "", "songqueue.add.full")
	case err != nil:
		qc.log.Warn("songqueue: add failed", qc.bid(), zap.Error(err))
		return err
	default:
		qc.reply(emit, qc.cfg.AddMessage, "songqueue.add.ok",
			"title", track.Name,
			"artist", strings.Join(track.Artists, ", "),
			"pos", strconv.Itoa(pos),
		)
	}
	return nil
}

// resolveTrack runs the gossip search and returns its top track, or the
// user-facing line explaining why none could be queued. A reply-level
// failure already carries chat-safe text (an unsupported share, no Spotify
// connection on file) and is surfaced verbatim; anything else is
// infrastructure: logged here where it is known, answered generically so an
// outage leaks no detail.
func (qc songQueueCmd) resolveTrack(ctx context.Context, query string) (*gossiprpc.SpotifyTrack, string) {
	var reply gossiprpc.SpotifySearchReply
	err := qc.gossip.Call(ctx,
		engine.GossipRoute{Provider: "spotify", Endpoint: "search"},
		gossiprpc.Request{
			ChannelID: strconv.FormatUint(qc.c.BroadcasterID, 10),
			Query:     query,
			Limit:     1,
		}, &reply)
	switch {
	case reply.Error != "":
		return nil, reply.Error
	case err != nil:
		qc.log.Warn("songqueue: search rpc failed",
			zap.String("query", query), qc.bid(), zap.Error(err))
		return nil, i18n.T(qc.c.Locale, "songqueue.err.upstream")
	case len(reply.Tracks) == 0:
		return nil, i18n.T(qc.c.Locale, "songqueue.search.none")
	}
	return &reply.Tracks[0], ""
}

// entry projects a resolved provider track onto a queue entry owned by the
// asking viewer.
func (qc songQueueCmd) entry(t gossiprpc.SpotifyTrack) engine.SongEntry {
	return engine.SongEntry{
		TrackID:       t.ID,
		Title:         t.Name,
		Artists:       t.Artists,
		DurationMS:    t.DurationMS,
		ArtworkURL:    t.ImageURL,
		URL:           t.URL,
		RequesterID:   qc.c.Env.ChatterUserID,
		RequesterName: qc.c.Env.ChatterName(),
	}
}

// retract takes back the chatter's own pending request, never anybody
// else's, never the playing track.
func (qc songQueueCmd) retract(ctx context.Context, emit module.Emit) error {
	entry, removed, err := qc.store.RetractOwn(ctx, qc.c.BroadcasterID, qc.c.Env.ChatterUserID)
	if err != nil {
		qc.log.Warn("songqueue: retract failed", qc.bid(), zap.Error(err))
		return err
	}
	if !removed {
		qc.reply(emit, "", "songqueue.retract.none")
		return nil
	}
	qc.reply(emit, qc.cfg.RetractMessage, "songqueue.retract.ok", "title", entry.Title)
	return nil
}

// removeAt drops a positional entry (mod path of "!sr remove <n>").
func (qc songQueueCmd) removeAt(ctx context.Context, pos int, emit module.Emit) error {
	entry, removed, err := qc.store.RemoveAt(ctx, qc.c.BroadcasterID, pos)
	if err != nil {
		qc.log.Warn("songqueue: remove-at failed", zap.Int("position", pos), qc.bid(), zap.Error(err))
		return err
	}
	if !removed {
		qc.reply(emit, "", "songqueue.remove.not_found", "pos", strconv.Itoa(pos))
		return nil
	}
	qc.reply(emit, "", "songqueue.remove.ok",
		"pos", strconv.Itoa(entry.Position),
		"title", entry.Title,
		"req", entry.RequesterName,
	)
	return nil
}

// nextTrack marks the head played and promotes the next entry.
func (qc songQueueCmd) nextTrack(ctx context.Context, emit module.Emit) error {
	_, now, err := qc.store.Advance(ctx, qc.c.BroadcasterID)
	if err != nil {
		qc.log.Warn("songqueue: advance failed", qc.bid(), zap.Error(err))
		return err
	}
	if now == nil {
		qc.reply(emit, "", "songqueue.next.empty")
		return nil
	}
	qc.reply(emit, qc.cfg.PlayingMessage, "songqueue.playing",
		"title", now.Title,
		"artist", strings.Join(now.Artists, ", "),
		"req", now.RequesterName,
	)
	return nil
}

func (qc songQueueCmd) clearAll(ctx context.Context, emit module.Emit) error {
	if err := qc.store.Clear(ctx, qc.c.BroadcasterID); err != nil {
		qc.log.Warn("songqueue: clear failed", qc.bid(), zap.Error(err))
		return err
	}
	qc.reply(emit, "", "songqueue.cleared")
	return nil
}

// view answers bare "!sr" with now-playing plus the next few requests.
func (qc songQueueCmd) view(ctx context.Context, emit module.Emit) error {
	snap, err := qc.store.Snapshot(ctx, qc.c.BroadcasterID, songqueueListLen)
	if err != nil {
		qc.log.Warn("songqueue: snapshot failed", qc.bid(), zap.Error(err))
		return err
	}
	if snap.Current == nil && len(snap.UpNext) == 0 {
		qc.reply(emit, "", "songqueue.status.empty")
		return nil
	}
	if snap.Current == nil {
		qc.reply(emit, "", "songqueue.status.queued",
			"list", renderSongLines(snap.UpNext),
			"count", strconv.Itoa(len(snap.UpNext)),
		)
		return nil
	}
	kv := []string{
		"title", snap.Current.Title,
		"req", snap.Current.RequesterName,
	}
	if len(snap.UpNext) > 0 {
		kv = append(kv,
			"list", renderSongLines(snap.UpNext),
			"count", strconv.Itoa(len(snap.UpNext)),
		)
		qc.reply(emit, "", "songqueue.status.playing", kv...)
		return nil
	}
	qc.reply(emit, "", "songqueue.status.current_only", kv...)
	return nil
}

// renderSongLines formats up-next entries as "1. Title (by Name)" joined
// with " · ", compact enough for one chat line.
func renderSongLines(entries []engine.SongEntry) string {
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteString(" · ")
		}
		b.WriteString(strconv.Itoa(e.Position))
		b.WriteString(". ")
		b.WriteString(e.Title)
		b.WriteString(" (by ")
		b.WriteString(e.RequesterName)
		b.WriteString(")")
	}
	return b.String()
}

// parsePosition reads a 1-based position argument; zero means "not a number".
func parsePosition(s string) int {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// emitChat sends one raw chat line that is not template-shaped (the provider
// already produced user-facing text).
func (qc songQueueCmd) emitChat(emit module.Emit, text string) {
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: qc.c.Env.BroadcasterUserID,
		Text:          text,
	})
}

// reply emits one chat line from a customizable override or the localized
// default: the queue module's mechanism, shared verbatim.
func (qc songQueueCmd) reply(emit module.Emit, override, key string, kv ...string) {
	tmpl := override
	if tmpl == "" {
		tmpl = i18n.T(qc.c.Locale, key)
	}
	text := module.ExpandString(tmpl, func(k string) (string, bool) {
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i] == k {
				return kv[i+1], true
			}
		}
		if k == "user" {
			return qc.c.Env.ChatterUserLogin, true
		}
		return module.ParseDynamic(k)
	})
	qc.emitChat(emit, text)
}

func (qc songQueueCmd) bid() zap.Field { return zap.Uint64("broadcaster_id", qc.c.BroadcasterID) }
