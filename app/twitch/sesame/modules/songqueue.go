// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"go.uber.org/zap"
)

const songqueueModuleName = "songqueue"

const songqueueListLen = 3

const srlistLen = 5

const (
	defaultSongQueueDepth = 100
	hardSongQueueDepth    = 1000
)

const srAddCooldown = 5 * time.Second

const currentCooldown = 5 * time.Second

type songqueueConfig struct {
	MaxDepth       int              `json:"maxDepth"`
	AddMessage     string           `json:"addMessage"`
	PlayingMessage string           `json:"playingMessage"`
	RetractMessage string           `json:"retractMessage"`
	CurrentMessage string           `json:"currentMessage"`
	Sr             *songqueueSr     `json:"sr"`
	Redeem         *songqueueRedeem `json:"redeem"`
	Quotas         *songqueueQuotas `json:"quotas"`
}

type songqueueQuotas struct {
	Everyone *int `json:"everyone"`
	Sub      *int `json:"sub"`
	VIP      *int `json:"vip"`
	Mod      *int `json:"mod"`
}

type songqueueSr struct {
	Enabled      *bool  `json:"enabled"`
	Perm         string `json:"perm"`
	AllowOffline bool   `json:"allowOffline"`
}

type songqueueRedeem struct {
	Enabled      bool   `json:"enabled"`
	RewardID     string `json:"rewardId"`
	OnRedeem     string `json:"onRedeem"`
	ReplyMessage string `json:"replyMessage"`
	AllowOffline bool   `json:"allowOffline"`
}

func SongQueue(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(songqueueModuleName, module.KindOptIn)
	m.Command("sr").Everyone().Cooldown(srAddCooldown).
		Aliases("songrequest", "songreq").
		Run(songQueueDispatch(d, log))

	m.Command("song").Everyone().Cooldown(currentCooldown).
		Aliases("current", "nowplaying", "np").
		Run(songQueueView(d, log))

	m.Command("skip").Mod().
		Aliases("next").
		Run(songQueueSkip(d, log))

	m.Command("clear").Mod().
		Run(songQueueClear(d, log))
	m.Command("remove").Everyone().
		Run(songQueueRemove(d, log))
	m.Command("srlist").Everyone().Cooldown(currentCooldown).
		Aliases("songlist").
		Run(songQueueList(d, log))

	m.On(redemptionAddType, songqueueRedemption(d, log))
	return m.Build()
}

type songQueueCmd struct {
	chatReplier
	store    engine.SongQueueStore
	gossip   engine.GossipCaller
	live     engine.IsLiveChecker
	cfg      songqueueConfig
	log      *zap.Logger
	maxDepth int
}

func newSongQueueCmd(d engine.Deps, c *module.Context, log *zap.Logger) (qc songQueueCmd, ok bool) {
	if d.SongQueue == nil {
		return songQueueCmd{}, false
	}
	qc = songQueueCmd{chatReplier: newChatReplier(c), store: d.SongQueue, gossip: d.Gossip, live: d.Live, log: log}
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

type songQueueAction func(qc *songQueueCmd, ctx context.Context, args, rest string, emit module.Emit) error

var songQueueActions = map[string]songQueueAction{
	"retract": (*songQueueCmd).actRetract,
	"cancel":  (*songQueueCmd).actRetract,
	"remove":  (*songQueueCmd).actRemove,
	"next":    (*songQueueCmd).actNext,
	"skip":    (*songQueueCmd).actNext,
	"clear":   (*songQueueCmd).actClear,
}

func songQueueView(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		qc, ok := newSongQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		return qc.current(ctx, emit)
	}
}

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
	key := replyKey("songqueue.current.ok")
	if req := qc.requesterOf(ctx, track.ID); req != "" {
		kv = append(kv, "req", req)
		key = "songqueue.current.req"
	}
	qc.reply(emit, qc.cfg.CurrentMessage, key, kv...)
	return nil
}

func (qc songQueueCmd) livePlayer(ctx context.Context) (*gossiprpc.SpotifyTrack, string) {
	if qc.gossip == nil {
		return nil, ""
	}
	var reply gossiprpc.SpotifyNowPlayingReply
	err := qc.gossip.Call(ctx,
		engine.GossipRoute{Provider: "spotify", Endpoint: "nowplaying"},
		gossiprpc.Request{ChannelID: strconv.FormatUint(qc.c.BroadcasterID, 10)}, &reply)
	if reply.Error != "" {
		return nil, reply.Error
	}
	if err != nil {
		qc.log.Warn("songqueue: nowplaying rpc failed", qc.c.BID(), zap.Error(err))
		return nil, i18n.T(qc.c.Locale, "songqueue.err.upstream")
	}
	if !reply.IsPlaying {
		return nil, ""
	}
	return reply.Track, ""
}

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

func (qc songQueueCmd) syncWithPlayer(ctx context.Context) {
	track, failure := qc.livePlayer(ctx)
	if failure != "" || track == nil {
		return
	}
	if _, err := qc.store.SyncPlaying(ctx, qc.c.BroadcasterID, track.ID); err != nil {
		qc.log.Warn("songqueue: player sync failed", qc.c.BID(), zap.Error(err))
	}
}

func songQueueSkip(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		qc, ok := newSongQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		return qc.nextTrack(ctx, emit)
	}
}

func songQueueClear(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		qc, ok := newSongQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		return qc.clearAll(ctx, emit)
	}
}

func songQueueRemove(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		qc, ok := newSongQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		return qc.actRemove(ctx, args, strings.TrimSpace(args), emit)
	}
}

func songQueueList(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		qc, ok := newSongQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		return qc.viewDepth(ctx, srlistLen, emit)
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

func (qc songQueueCmd) bareModVerb(ctx context.Context, args, rest string, emit module.Emit, mod func(context.Context, module.Emit) error) error {
	if rest != "" {
		return qc.request(ctx, args, emit)
	}
	if !qc.c.Chatter().Allows(module.RoleModerator) {
		return nil
	}
	return mod(ctx, emit)
}

func (qc songQueueCmd) request(ctx context.Context, query string, emit module.Emit) error {
	if !qc.canRequest() {
		return nil
	}
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
	qc.syncWithPlayer(ctx)
	pos, err := qc.store.Add(ctx, qc.c.BroadcasterID, qc.entry(*track), engine.SongQueueLimits{MaxDepth: qc.maxDepth, PerRequester: qc.quotaFor()})
	if err != nil {
		return qc.reportAdd(emit, *track, pos, err)
	}
	if failure := qc.pushToPlayer(ctx, track.ID); failure != "" {
		if _, _, rbErr := qc.store.RetractOwn(ctx, qc.c.BroadcasterID, qc.c.Env.ChatterUserID); rbErr != nil {
			qc.log.Warn("songqueue: rollback after player refusal failed", qc.c.BID(), zap.Error(rbErr))
		}
		qc.emitChat(emit, failure)
		return nil
	}
	return qc.reportAdd(emit, *track, pos, nil)
}

func (qc songQueueCmd) srRefusal() replyKey {
	if qc.cfg.Sr == nil {
		return ""
	}
	if qc.cfg.Sr.Enabled != nil && !*qc.cfg.Sr.Enabled {
		return "songqueue.sr.off"
	}
	if !qc.c.Chatter().Allows(module.ParsePerm(qc.cfg.Sr.Perm)) {
		return "songqueue.sr.perm"
	}
	return ""
}

func (qc songQueueCmd) chatLiveOK(ctx context.Context) bool {
	if qc.cfg.Sr == nil {
		return true
	}
	return qc.livePermits(ctx, qc.cfg.Sr.AllowOffline)
}

func (qc songQueueCmd) livePermits(ctx context.Context, allowOffline bool) bool {
	if allowOffline {
		return true
	}
	if qc.live == nil {
		return true
	}
	ok, err := qc.live.IsLive(ctx, qc.c.BroadcasterID)
	if err != nil {
		qc.log.Warn("songqueue: live check failed, denying", qc.c.BID(), zap.Error(err))
		return false
	}
	return ok
}

func (qc songQueueCmd) quotaFor() int {
	q := qc.cfg.Quotas
	if q == nil {
		return 0
	}
	var tier *int
	switch role := qc.c.Chatter(); {
	case role.Allows(module.RoleBroadcaster):
		return 0
	case role.Allows(module.RoleModerator):
		tier = q.Mod
	case role.Allows(module.RoleVIP):
		tier = q.VIP
	case role.Allows(module.RoleSubscriber):
		tier = q.Sub
	default:
		tier = q.Everyone
	}
	if tier == nil || *tier <= 0 {
		return 0
	}
	return *tier
}

func (qc songQueueCmd) pushToPlayer(ctx context.Context, trackID string) string {
	if qc.gossip == nil {
		return i18n.T(qc.c.Locale, "songqueue.err.upstream")
	}
	var reply gossiprpc.SpotifyPlayerReply
	err := qc.gossip.Call(ctx,
		engine.GossipRoute{Provider: "spotify", Endpoint: "queue"},
		gossiprpc.Request{ChannelID: strconv.FormatUint(qc.c.BroadcasterID, 10), TrackID: trackID}, &reply)
	if reply.Error != "" {
		return reply.Error
	}
	if err != nil {
		qc.log.Warn("songqueue: player queue push failed", qc.c.BID(), zap.Error(err))
		return i18n.T(qc.c.Locale, "songqueue.err.upstream")
	}
	return ""
}

func (qc songQueueCmd) skipPlayer(ctx context.Context) string {
	if qc.gossip == nil {
		return i18n.T(qc.c.Locale, "songqueue.err.upstream")
	}
	var reply gossiprpc.SpotifyPlayerReply
	err := qc.gossip.Call(ctx,
		engine.GossipRoute{Provider: "spotify", Endpoint: "next"},
		gossiprpc.Request{ChannelID: strconv.FormatUint(qc.c.BroadcasterID, 10)}, &reply)
	if reply.Error != "" {
		return reply.Error
	}
	if err != nil {
		qc.log.Warn("songqueue: player skip failed", qc.c.BID(), zap.Error(err))
		return i18n.T(qc.c.Locale, "songqueue.err.upstream")
	}
	return ""
}

func (qc songQueueCmd) canRequest() bool {
	return qc.gossip != nil && qc.c.Env.ChatterUserID != ""
}

func (qc songQueueCmd) reportAdd(emit module.Emit, track gossiprpc.SpotifyTrack, pos int, err error) error {
	switch {
	case errors.Is(err, engine.ErrSongQuotaReached):
		qc.reply(emit, "", "songqueue.add.quota", "limit", strconv.Itoa(qc.quotaFor()))
	case errors.Is(err, engine.ErrSongQueueFull):
		qc.reply(emit, "", "songqueue.add.full")
	case err != nil:
		qc.log.Warn("songqueue: add failed", qc.c.BID(), zap.Error(err))
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
			zap.String("query", query), qc.c.BID(), zap.Error(err))
		return nil, i18n.T(qc.c.Locale, "songqueue.err.upstream")
	case len(reply.Tracks) == 0:
		return nil, i18n.T(qc.c.Locale, "songqueue.search.none")
	}
	return &reply.Tracks[0], ""
}

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

func (qc songQueueCmd) retract(ctx context.Context, emit module.Emit) error {
	entry, removed, err := qc.store.RetractOwn(ctx, qc.c.BroadcasterID, qc.c.Env.ChatterUserID)
	if err != nil {
		qc.log.Warn("songqueue: retract failed", qc.c.BID(), zap.Error(err))
		return err
	}
	if !removed {
		qc.reply(emit, "", "songqueue.retract.none")
		return nil
	}
	qc.reply(emit, qc.cfg.RetractMessage, "songqueue.retract.ok", "title", entry.Title)
	return nil
}

func (qc songQueueCmd) removeAt(ctx context.Context, pos int, emit module.Emit) error {
	entry, removed, err := qc.store.RemoveAt(ctx, qc.c.BroadcasterID, pos)
	if err != nil {
		qc.log.Warn("songqueue: remove-at failed", zap.Int("position", pos), qc.c.BID(), zap.Error(err))
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

func (qc songQueueCmd) nextTrack(ctx context.Context, emit module.Emit) error {
	if failure := qc.skipPlayer(ctx); failure != "" {
		qc.emitChat(emit, failure)
		return nil
	}
	_, now, err := qc.store.Advance(ctx, qc.c.BroadcasterID)
	if err != nil {
		qc.log.Warn("songqueue: advance failed", qc.c.BID(), zap.Error(err))
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
		qc.log.Warn("songqueue: clear failed", qc.c.BID(), zap.Error(err))
		return err
	}
	qc.reply(emit, "", "songqueue.cleared")
	return nil
}

func (qc songQueueCmd) view(ctx context.Context, emit module.Emit) error {
	return qc.viewDepth(ctx, songqueueListLen, emit)
}

func (qc songQueueCmd) viewDepth(ctx context.Context, depth int, emit module.Emit) error {
	qc.syncWithPlayer(ctx)
	snap, err := qc.store.Snapshot(ctx, qc.c.BroadcasterID, depth)
	if err != nil {
		qc.log.Warn("songqueue: snapshot failed", qc.c.BID(), zap.Error(err))
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

func parsePosition(s string) int {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func (qc songQueueCmd) emitChat(emit module.Emit, text string) {
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: qc.c.Env.BroadcasterUserID,
		Text:          text,
	})
}
