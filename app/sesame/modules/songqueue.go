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
	"ItsBagelBot/pkg/bus"

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
		isMod := c.Chatter().Allows(module.RoleModerator)

		switch strings.ToLower(sub) {
		case "retract", "cancel":
			return qc.retract(ctx, emit)
		case "remove":
			// A number aims at someone else's position (mod-only); anything
			// else — including a non-mod guessing at numbers — retracts the
			// chatter's own request, which is always safe.
			if n := parsePosition(rest); n > 0 && isMod {
				return qc.removeAt(ctx, n, emit)
			}
			return qc.retract(ctx, emit)
		case "next", "clear":
			// Bare-word verbs only: with anything trailing, the whole input
			// reads back as a query, so song titles starting with these words
			// still resolve instead of being eaten as a mod command.
			if rest != "" {
				return qc.request(ctx, args, emit)
			}
			if !isMod {
				return nil
			}
			if strings.ToLower(sub) == "next" {
				return qc.nextTrack(ctx, emit)
			}
			return qc.clearAll(ctx, emit)
		default:
			return qc.request(ctx, args, emit)
		}
	}
}

// request resolves free text / links to exactly one track via the gossip
// spotify provider and queues it for the asking viewer.
func (qc songQueueCmd) request(ctx context.Context, query string, emit module.Emit) error {
	if qc.gossip == nil || qc.c.Env.ChatterUserID == "" {
		return nil
	}
	var reply gossiprpc.SpotifySearchReply
	err := qc.gossip.Call(ctx,
		engine.GossipRoute{Provider: "spotify", Endpoint: "search"},
		gossiprpc.Request{
			ChannelID: strconv.FormatUint(qc.c.BroadcasterID, 10),
			Query:     query,
			Limit:     1,
		}, &reply)
	if err != nil {
		// A provider-level failure ("no Spotify connection on file", "that
		// link type isn't supported") is already chat-safe text; surface it
		// verbatim. Anything else is infrastructure — say so generically.
		var rpcErr *bus.RPCReplyError
		if errors.As(err, &rpcErr) && reply.Error != "" {
			qc.emitChat(emit, reply.Error)
			return nil
		}
		qc.log.Warn("songqueue: search rpc failed",
			zap.String("query", query), qc.bid(), zap.Error(err))
		qc.reply(emit, "", "songqueue.err.upstream")
		return nil
	}
	if reply.Error != "" {
		qc.emitChat(emit, reply.Error)
		return nil
	}
	if len(reply.Tracks) == 0 {
		qc.reply(emit, "", "songqueue.search.none")
		return nil
	}

	track := reply.Tracks[0]
	pos, err := qc.store.Add(ctx, qc.c.BroadcasterID, engine.SongEntry{
		TrackID:       track.ID,
		Title:         track.Name,
		Artists:       track.Artists,
		DurationMS:    track.DurationMS,
		ArtworkURL:    track.ImageURL,
		URL:           track.URL,
		RequesterID:   qc.c.Env.ChatterUserID,
		RequesterName: qc.c.Env.ChatterName(),
	}, qc.maxDepth)
	switch {
	case errors.Is(err, engine.ErrSongAlreadyQueued):
		qc.reply(emit, "", "songqueue.add.already")
		return nil
	case errors.Is(err, engine.ErrSongQueueFull):
		qc.reply(emit, "", "songqueue.add.full")
		return nil
	case err != nil:
		qc.log.Warn("songqueue: add failed", qc.bid(), zap.Error(err))
		return err
	}
	qc.reply(emit, qc.cfg.AddMessage, "songqueue.add.ok",
		"title", track.Name,
		"artist", strings.Join(track.Artists, ", "),
		"pos", strconv.Itoa(pos),
	)
	return nil
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
