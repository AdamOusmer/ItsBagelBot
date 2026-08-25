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

// songqueueConfig holds the broadcaster's overrides. MaxDepth caps the
// pending line; the message fields are dashboard-editable templates whose
// empty value falls back to the localized default next to each.
type songqueueConfig struct {
	MaxDepth       int    `json:"maxDepth"`
	AddMessage     string `json:"addMessage"`     // i18n songqueue.add.ok   {user} {title} {artist} {pos}
	PlayingMessage string `json:"playingMessage"` // i18n songqueue.playing  {title} {artist} {req}
	RetractMessage string `json:"retractMessage"` // i18n songqueue.retract.ok {user} {title}
}

// SongQueue owns the viewer song-request queue, resolved against the
// broadcaster's connected Spotify account through the gossip spotify
// provider. It is opt-in like the player queue; on channels that never enable
// it the !sr spelling falls through to custom commands untouched.
//
//	!sr <song|artist - song|spotify link> → resolve + queue (one per viewer)
//	!sr                                   → now playing + the next few
//	!sr remove / retract / cancel         → take back YOUR queued request
//	!sr remove <position>                 → mod: remove anyone's entry
//	!sr next                              → mod: mark played, promote next
//	!sr clear                             → mod: empty everything
//
// next/clear are verbs only as bare words — "next episode by dr. dre" is a
// request for a song, not a skip.
//
// Retraction keys on the chatter's Twitch user id captured at request time,
// never the display name, and only ever touches that viewer's own pending
// entry — "the one they asked" and nothing else. The currently-playing track
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
	// bare invocation, which would make every one of them mean "view" — !skip
	// has to advance the queue.
	//
	// NOT !queue: the viewer queue module (queue.go) already owns that name, and
	// command precedence is registration order in All(), so claiming it here
	// would shadow a different feature on any channel running both. !song and
	// !current say what this one is about anyway.
	//
	// No cooldown: a read and a mod action, neither spends the Spotify lookup an
	// add does.
	m.Command("song").Everyone().
		Aliases("current", "nowplaying", "np").
		Run(songQueueView(d, log))

	// !skip carries its moderator gate on the registration, where the !sr next
	// sub-verb has to enforce it by hand — a bare word after !sr could also be
	// a song title, so that path cannot lean on the command's own permission.
	m.Command("skip").Mod().
		Run(songQueueSkip(d, log))
	return m.Build()
}

// songQueueCmd bundles the per-invocation state every handler shares, built
// once by newSongQueueCmd — the same shape as queueCmd.
type songQueueCmd struct {
	store    engine.SongQueueStore
	gossip   engine.GossipCaller
	c        *module.Context
	cfg      songqueueConfig
	log      *zap.Logger
	maxDepth int
}

func newSongQueueCmd(d engine.Deps, c *module.Context, log *zap.Logger) (qc songQueueCmd, ok bool) {
	if d.SongQueue == nil {
		return songQueueCmd{}, false
	}
	qc = songQueueCmd{store: d.SongQueue, gossip: d.Gossip, c: c, log: log}
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
	"clear":   (*songQueueCmd).actClear,
}

// songQueueView backs !song / !current / !nowplaying / !np: the same read !sr
// gives with no arguments.
func songQueueView(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		qc, ok := newSongQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		return qc.view(ctx, emit)
	}
}

// songQueueSkip backs !skip: the same advance !sr next performs. The moderator
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
// anything else — including a non-mod guessing at numbers — retracts the
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
	track, failure := qc.resolveTrack(ctx, query)
	if failure != "" {
		qc.emitChat(emit, failure)
		return nil
	}
	pos, err := qc.store.Add(ctx, qc.c.BroadcasterID, qc.entry(*track), qc.maxDepth)
	return qc.reportAdd(emit, *track, pos, err)
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
// infrastructure — logged here where it is known, answered generically so an
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

// retract takes back the chatter's own pending request — never anybody
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
// default — the queue module's mechanism, shared verbatim.
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
