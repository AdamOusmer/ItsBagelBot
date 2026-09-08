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
	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

// Shared machinery for the external-stats modules (valorant, clashroyale,
// fortnite, urchin, mcsr). Every one of them answers a chat command the same
// way — decode the module config, check that command's toggle, resolve which
// account the lookup is about, call gossip, chat an upstream failure, expand a
// template over the reply — and each had grown its own copy of that sequence
// alongside its own account block, its own subcommand switch and its own token
// closure. statsHandler below is the one skeleton; the pieces that genuinely
// differ per provider are injected.

// linkedAccountConfig is the account block every external-stats module's
// dashboard config carries: the handle the broadcaster linked, the uuid
// resolved next to it, and the "only look up my linked account" toggle.
//
// It is embedded rather than respelled per module so the json tags cannot
// drift — the console writes one shape for all of them, and a tag renamed on
// one module would read as unset rather than fail. AccountUUID stays in the
// shared block even though only the Minecraft-backed modules ever store one:
// a key absent from a config blob decodes to "", and PreferUUID is what
// decides whether it is consulted at all.
type linkedAccountConfig struct {
	Account     string `json:"account"`
	AccountUUID string `json:"accountUuid"`
	LinkedOnly  string `json:"linkedOnly"`
}

// linked returns the block itself, satisfying linkedConfig for every config
// that embeds it.
func (l linkedAccountConfig) linked() linkedAccountConfig { return l }

// linkedConfig is what the shared account resolution needs of a decoded module
// config: that it carries the linked-account block. A method rather than a
// struct field so a generic function can reach it — Go has no field
// constraint.
type linkedConfig interface{ linked() linkedAccountConfig }

// accountSources is the fallback chain resolveAccount picks from, highest
// priority first: the argument the viewer typed, the module's linked account
// (uuid when PreferUUID and one is stored), and the broadcaster's own Twitch
// login.
type accountSources struct {
	Arg              string
	Linked           string
	LinkedUUID       string
	BroadcasterLogin string
	// PreferUUID uses LinkedUUID for the linked-account path when the
	// upstream accepts or requires a uuid (Hypixel requires one; Urchin and
	// MCSR Ranked accept one). PaceMan is name-keyed, so those commands leave
	// this false and keep the typed username.
	PreferUUID bool
	// LinkedOnly drops Arg so a viewer cannot point the command at another
	// player: the broadcaster's "only look up my linked account" toggle. The
	// ignore is silent by design (the viewer gets the broadcaster's stats
	// rather than a refusal line), so no i18n key exists for it.
	LinkedOnly bool
}

// resolveAccount picks the account a stats command targets, in priority order:
// an explicit argument typed after the command (first word, '@' stripped), the
// module's configured linked account (uuid when PreferUUID and one is stored),
// then the broadcaster's own Twitch login (the "default linked account per
// user" — most streamers use the same handle). LinkedOnly skips the first
// step entirely.
func resolveAccount(s accountSources) string {
	if first, _, _ := strings.Cut(strings.TrimSpace(s.Arg), " "); first != "" && !s.LinkedOnly {
		return strings.TrimPrefix(first, "@")
	}
	if linked := linkedAccount(s); linked != "" {
		return linked
	}
	return s.BroadcasterLogin
}

// linkedAccount is the module's configured identity: the stored uuid when
// PreferUUID and one exists, otherwise the typed username.
func linkedAccount(s accountSources) string {
	uuid := strings.TrimSpace(s.LinkedUUID)
	if s.PreferUUID && uuid != "" {
		return uuid
	}
	return strings.TrimSpace(s.Linked)
}

// resolveLinked fills BroadcasterLogin from the chat context and resolves
// both the lookup account and the chat-facing display name. Every urchin/mcsr
// linked-account path goes through here so the uuid-vs-name choice lives in
// one literal rather than five copies. account may be a stored uuid
// (PreferUUID); display walks the same fallback chain with the uuid ignored,
// so an error line chats the username the broadcaster typed rather than 32
// hex characters.
func resolveLinked(c *module.Context, s accountSources) (account, display string) {
	s.BroadcasterLogin = c.Env.BroadcasterUserLogin
	account = resolveAccount(s)
	s.PreferUUID = false
	return account, resolveAccount(s)
}

// chatReplyError turns a gossip failure into a chat line so the viewer gets an
// answer instead of silence. A reply-level failure (player not found, rate
// limited) chats gossip's own message and reports handled=true. An
// infrastructure failure (timeout, no responder) also chats — a cold lookup can
// outlive sesame's RPC budget while gossip finishes and caches, so telling
// the viewer to retry is exactly right — but reports handled=false so the
// caller still propagates the error for logging.
func chatReplyError(c *module.Context, emit module.Emit, account string, err error) bool {
	var re bus.RPCReplyError
	if errors.As(err, &re) {
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          account + ": " + re.Message,
		})
		return true
	}
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          account + ": " + i18n.T(c.Locale, "external.retry"),
	})
	return false
}

// ratio renders kills/deaths style ratios with two decimals; a zero
// denominator counts as one so a flawless run shows the raw numerator.
func ratio(num, den int64) string {
	if den == 0 {
		den = 1
	}
	return strconv.FormatFloat(float64(num)/float64(den), 'f', 2, 64)
}

// signed renders a delta with an explicit sign so "gained 12" and "lost 12"
// never read the same ("+12", "-12", "±0").
func signed(n int) string {
	switch {
	case n > 0:
		return "+" + strconv.Itoa(n)
	case n < 0:
		return strconv.Itoa(n)
	default:
		return "±0"
	}
}

// orDefault returns tmpl unless it is blank, then def.
func orDefault(tmpl, def string) string {
	if strings.TrimSpace(tmpl) == "" {
		return def
	}
	return tmpl
}

// i64 renders an int64 for template tokens.
func i64(n int64) string { return strconv.FormatInt(n, 10) }

// trimScore renders a float score without trailing zero noise (7.50 -> 7.5,
// 3.00 -> 3).
func trimScore(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// statsCall is one invocation of a stats command: the chat context, the
// decoded module config, and whatever the viewer typed after the command.
// Bundled into one value because every injected strategy needs all three, and
// threading them as separate parameters is what pushed the per-provider
// helpers past a readable signature.
type statsCall[C any] struct {
	Ctx  *module.Context
	Cfg  C
	Args string
}

// statsSubject is who one lookup is about. Account is what the upstream is
// asked for and may be a stored uuid; Display is what a failure chats, and is
// always the human-readable name — an error line naming 32 hex characters is
// not an answer.
type statsSubject struct {
	Account string
	Display string

	// AccountB is the other side of a lookup that compares two players
	// (mcsr's !record). It stays empty for every single-account command and is
	// read only by a request strategy that asks for it; resolving both sides
	// here is what keeps the pair and the name a failure chats from being
	// derived twice per call.
	AccountB string

	// Refusal is the line to chat instead of calling upstream, for arguments
	// that name no answerable subject (!record with nobody to compare
	// against). It rides the subject because resolving one is where a command
	// first learns its arguments are unusable, and because the refusal must
	// come after the module's own toggle has been checked — a command the
	// broadcaster turned off answers nothing at all, usage line included.
	Refusal string
}

// statsHandler is the shape every external-stats command shares.
//
// Template Method: run below is the fixed skeleton (decode, toggle, resolve,
// call, chat an upstream error, render, emit) and never varies. Strategy: the
// three function fields are the steps that do — who the lookup targets, what
// the upstream is asked, and how its reply becomes a chat line. Five modules
// had each written that skeleton out, which is what CodeScene's duplication
// finding flagged on the pair that shipped first.
type statsHandler[C any, R any] struct {
	d engine.Deps

	// enabled reads the command's own toggle field off the decoded config.
	enabled func(C) string
	// route names the gossip provider and endpoint this command calls.
	route engine.GossipRoute
	// target resolves who the lookup is about.
	target func(statsCall[C]) statsSubject
	// request builds the gossip request for that target.
	request func(statsCall[C], statsSubject) gossiprpc.Request
	// render turns a decoded reply into the chat line to send.
	render func(statsCall[C], *R) string
}

// run implements module.RunFunc, so a handler can be handed straight to
// Command(...).Run; commands that pre-process their typed args (peeling off a
// "season:<n>" token, or discarding the args entirely) call it directly.
func (h statsHandler[C, R]) run(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
	var cfg C
	_ = c.Decode(&cfg)
	if !alertOn(h.enabled(cfg)) || h.d.Gossip == nil {
		return nil
	}

	call := statsCall[C]{Ctx: c, Cfg: cfg, Args: args}
	subject := h.target(call)
	if subject.Refusal != "" {
		emitChat(c, emit, subject.Refusal)
		return nil
	}

	var reply R
	if err := h.d.Gossip.Call(ctx, h.route, h.request(call, subject), &reply); err != nil {
		return gossipCallErr(c, emit, subject.Display, err)
	}
	emitChat(c, emit, h.render(call, &reply))
	return nil
}

// gossipCallErr maps a gossip failure onto the command's outcome: a
// reply-level error (player not found, rate limited) was answered in chat and
// counts as handled, while an infrastructure error also chats a retry hint but
// propagates so the caller still logs it.
func gossipCallErr(c *module.Context, emit module.Emit, display string, err error) error {
	if chatReplyError(c, emit, display, err) {
		return nil
	}
	return err
}

// emitChat sends text as a chat Output — the one shape every stats reply
// resolves to, whether it came from an expanded template or a plain i18n line.
func emitChat(c *module.Context, emit module.Emit, text string) {
	emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
}

// linkedTarget is the account-resolution strategy the linked-account commands
// share: the id the viewer typed, else the broadcaster's linked account, else
// their Twitch login. preferUUID picks the stored uuid for uuid-keyed
// upstreams (Hypixel requires one, MCSR Ranked and Urchin accept one); the
// name-keyed ones (PaceMan) pass false.
func linkedTarget[C linkedConfig](preferUUID bool) func(statsCall[C]) statsSubject {
	return func(call statsCall[C]) statsSubject {
		l := call.Cfg.linked()
		account, display := resolveLinked(call.Ctx, accountSources{
			Arg: call.Args, Linked: l.Account, LinkedUUID: l.AccountUUID,
			PreferUUID: preferUUID, LinkedOnly: explicitOn(l.LinkedOnly),
		})
		return statsSubject{Account: account, Display: display}
	}
}

// fixedSubject is the one way this package says "no account scopes this
// lookup" (fortnite's item shop, valorant's daily rotation): nothing is
// resolved, and name is what a failure chats about instead of a player. The
// empty Account it leaves behind is what accountRequest then sends, so those
// commands need no request strategy of their own.
func fixedSubject[C any](name string) func(statsCall[C]) statsSubject {
	return func(statsCall[C]) statsSubject { return statsSubject{Display: name} }
}

// accountRequest is the request every account-only command sends: the resolved
// account plus the caller's premium lane.
func accountRequest[C any](call statsCall[C], subject statsSubject) gossiprpc.Request {
	return gossiprpc.Request{Account: subject.Account, IsPremium: call.Ctx.Regress.IsPremium()}
}

// externalCommand is one linked-account stats command's whole wiring: the
// gossip route that answers it, the toggle that gates it, where its stored
// template lives, the default that template falls back to, the token palette
// the reply expands through, and the optional empty-state override.
//
// A command whose lookup is scoped by something the shared resolution knows
// nothing about (valorant's shard and ladder words) builds a statsHandler from
// handler() and replaces the two strategies it needs to, rather than growing
// this struct another knob.
type externalCommand[C linkedConfig, R any] struct {
	route    engine.GossipRoute
	enabled  func(C) string
	message  func(C) string
	fallback string
	tokens   module.TokenExpander[R]

	// special replaces the template entirely when the reply carries nothing
	// renderable (an unranked player, an empty board, a player who has not
	// played a match this season): every numeric token would print zero,
	// which reads as a wrong answer rather than no answer.
	//
	// It takes the whole call, like every other strategy here, because that
	// line is usually a translated sentence and the channel's locale hangs
	// off the context: a hook handed only the reply can answer nothing but
	// English.
	special func(statsCall[C], *R) (string, bool)

	// preferName keeps the linked username even when a uuid is stored, for
	// upstreams whose API is name-keyed and rejects uuids.
	preferName bool
}

// handler binds the command to the shared skeleton.
func (e externalCommand[C, R]) handler(d engine.Deps) statsHandler[C, R] {
	return statsHandler[C, R]{
		d:       d,
		enabled: e.enabled,
		route:   e.route,
		target:  linkedTarget[C](!e.preferName),
		request: accountRequest[C],
		render:  e.render,
	}
}

// run is the module.RunFunc that answers this command.
func (e externalCommand[C, R]) run(d engine.Deps) module.RunFunc { return e.handler(d).run }

// render is the handler's render strategy: the empty-state override when it
// fires, otherwise the configured template expanded over the reply.
func (e externalCommand[C, R]) render(call statsCall[C], reply *R) string {
	if text, ok := specialText(e.special, call, reply); ok {
		return text
	}
	return e.tokens.Expand(orDefault(e.message(call.Cfg), e.fallback), reply)
}

// specialText applies a command's empty-state override, if one is wired.
func specialText[C any, R any](special func(statsCall[C], *R) (string, bool), call statsCall[C], reply *R) (string, bool) {
	if special == nil {
		return "", false
	}
	return special(call, reply)
}

// ignoreArgs discards whatever the viewer typed. The session commands always
// target the linked account: their baseline is stored per channel and keyed to
// it, so honoring an arbitrary player would answer about somebody else's
// numbers against the streamer's snapshot. Per-player lookups have their own
// commands.
func ignoreArgs(run module.RunFunc) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		return run(ctx, c, "", emit)
	}
}

// subDispatch routes a root command's first argument word onto a named
// subcommand ("!cr decks #P2LQ0GR" runs decks over "#P2LQ0GR"). An unknown
// word is not an error: it is the default view's own argument, which is why
// the fallback is handed the untouched args and not the remainder — "!cr
// #P2LQ0GR" and "!val Frosty#EUW1" have to read naturally.
func subDispatch(fallback module.RunFunc, subs map[string]module.RunFunc) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		sub, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
		if run, ok := subs[strings.ToLower(sub)]; ok {
			return run(ctx, c, rest, emit)
		}
		return fallback(ctx, c, args, emit)
	}
}

// snapshotTimeout bounds the fire-and-forget stream-lifecycle snapshot calls.
const snapshotTimeout = 10 * time.Second

// snapshotSpec is one module's stream-lifecycle baseline: gossip stores the
// linked account's standing when the stream goes online and clears it when the
// stream ends, so a "this stream" command diffs against exactly this session
// and a rapid stop/restart cycle (#561) cannot leave it diffing the new stream
// against the old one's snapshot.
type snapshotSpec[C any, R any] struct {
	// provider names the gossip provider that owns the baseline; the two
	// endpoints under it are always session_start and session_end.
	provider string
	// enabled gates both halves on the module's own toggle. mcsr snapshots
	// unconditionally (the pipeline only runs the handler for an enabled
	// module); fortnite also checks its session toggle, because that upstream's
	// daily budget is tight enough that a command the broadcaster turned off
	// should not spend it.
	enabled func(C) bool
	// request builds the session_start call for the resolved channel id.
	request func(*module.Context, C, string) gossiprpc.Request
	// stored names what a successful snapshot logged, so each module keeps the
	// field that is worth reading back in its own logs.
	stored func(*R) zap.Field
}

// snapshotHandlers returns the stream.online / stream.offline pair for a
// module with a per-stream baseline. Both fire and forget on a Background
// context — the consumer's ctx is acked and may cancel the moment the handler
// returns — and both are sequenced per broadcaster behind every other
// lifecycle effect. fortnite and mcsr had grown byte-identical copies of this
// pair, comments included.
func snapshotHandlers[C any, R any](d engine.Deps, spec snapshotSpec[C, R]) (online, offline module.EventHandler) {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return spec.onlineHandler(d, log), spec.offlineHandler(d, log)
}

// onlineHandler snapshots the linked account's standing when the stream starts.
func (spec snapshotSpec[C, R]) onlineHandler(d engine.Deps, log *zap.Logger) module.EventHandler {
	return func(_ context.Context, c *module.Context, _ module.Emit) error {
		cfg, channelID, ok := spec.begin(d, c)
		if !ok {
			return nil
		}
		req := spec.request(c, cfg, channelID)
		seqOrGo(d.Seq, c.BroadcasterID, log, func() {
			wctx, cancel := context.WithTimeout(context.Background(), snapshotTimeout)
			defer cancel()
			var reply R
			route := engine.GossipRoute{Provider: spec.provider, Endpoint: "session_start"}
			if err := d.Gossip.Call(wctx, route, req, &reply); err != nil {
				log.Warn(spec.provider+": stream-start snapshot failed",
					zap.String("channel_id", channelID), zap.String("account", req.Account), zap.Error(err))
				return
			}
			log.Debug(spec.provider+": stream-start snapshot stored",
				zap.String("channel_id", channelID), zap.String("account", req.Account), spec.stored(&reply))
		})
		return nil
	}
}

// offlineHandler clears the channel's baseline when the stream ends. A gossip
// deployment without the provider (or in shop-only mode) answers no-responder,
// which is an expected miss, hence Debug rather than Warn.
func (spec snapshotSpec[C, R]) offlineHandler(d engine.Deps, log *zap.Logger) module.EventHandler {
	return func(_ context.Context, c *module.Context, _ module.Emit) error {
		_, channelID, ok := spec.begin(d, c)
		if !ok {
			return nil
		}
		seqOrGo(d.Seq, c.BroadcasterID, log, func() {
			wctx, cancel := context.WithTimeout(context.Background(), snapshotTimeout)
			defer cancel()
			var reply R
			route := engine.GossipRoute{Provider: spec.provider, Endpoint: "session_end"}
			if err := d.Gossip.Call(wctx, route, gossiprpc.Request{ChannelID: channelID}, &reply); err != nil {
				log.Debug(spec.provider+": stream-end snapshot clear failed",
					zap.String("channel_id", channelID), zap.Error(err))
			}
		})
		return nil
	}
}

// begin is the gate both halves share: gossip present, config decoded, toggle
// on. ok=false means the handler does nothing at all.
func (spec snapshotSpec[C, R]) begin(d engine.Deps, c *module.Context) (cfg C, channelID string, ok bool) {
	if d.Gossip == nil {
		return cfg, "", false
	}
	_ = c.Decode(&cfg)
	if !spec.enabled(cfg) {
		return cfg, "", false
	}
	return cfg, strconv.FormatUint(c.BroadcasterID, 10), true
}
