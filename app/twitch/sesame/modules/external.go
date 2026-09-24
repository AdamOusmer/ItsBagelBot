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

type linkedAccountConfig struct {
	Account     string `json:"account"`
	AccountUUID string `json:"accountUuid"`
	LinkedOnly  string `json:"linkedOnly"`
}

func (l linkedAccountConfig) linked() linkedAccountConfig { return l }

type linkedConfig interface{ linked() linkedAccountConfig }

type accountSources struct {
	Arg              string
	Linked           string
	LinkedUUID       string
	BroadcasterLogin string
	PreferUUID       bool
	LinkedOnly       bool
}

func resolveAccount(s accountSources) string {
	if first, _, _ := strings.Cut(strings.TrimSpace(s.Arg), " "); first != "" && !s.LinkedOnly {
		return strings.TrimPrefix(first, "@")
	}
	if linked := linkedAccount(s); linked != "" {
		return linked
	}
	return s.BroadcasterLogin
}

func linkedAccount(s accountSources) string {
	uuid := strings.TrimSpace(s.LinkedUUID)
	if s.PreferUUID && uuid != "" {
		return uuid
	}
	return strings.TrimSpace(s.Linked)
}

func resolveLinked(c *module.Context, s accountSources) (account, display string) {
	s.BroadcasterLogin = c.Env.BroadcasterUserLogin
	account = resolveAccount(s)
	s.PreferUUID = false
	return account, resolveAccount(s)
}

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

func ratio(num, den int64) string {
	if den == 0 {
		den = 1
	}
	return strconv.FormatFloat(float64(num)/float64(den), 'f', 2, 64)
}

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

func orDefault(tmpl, def string) string {
	if strings.TrimSpace(tmpl) == "" {
		return def
	}
	return tmpl
}

func i64(n int64) string { return strconv.FormatInt(n, 10) }

func trimScore(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

type statsCall[C any] struct {
	Ctx  *module.Context
	Cfg  C
	Args string
}

type statsSubject struct {
	Account string
	Display string

	AccountB string

	Refusal string
}

type statsHandler[C any, R any] struct {
	d engine.Deps

	enabled func(C) string
	route   engine.GossipRoute
	target  func(statsCall[C]) statsSubject
	request func(statsCall[C], statsSubject) gossiprpc.Request
	render  func(statsCall[C], *R) string
}

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

func gossipCallErr(c *module.Context, emit module.Emit, display string, err error) error {
	if chatReplyError(c, emit, display, err) {
		return nil
	}
	return err
}

func emitChat(c *module.Context, emit module.Emit, text string) {
	emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
}

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

func fixedSubject[C any](name string) func(statsCall[C]) statsSubject {
	return func(statsCall[C]) statsSubject { return statsSubject{Display: name} }
}

func accountRequest[C any](call statsCall[C], subject statsSubject) gossiprpc.Request {
	return gossiprpc.Request{Account: subject.Account, IsPremium: call.Ctx.Regress.IsPremium()}
}

type externalCommand[C linkedConfig, R any] struct {
	route    engine.GossipRoute
	enabled  func(C) string
	message  func(C) string
	fallback string
	tokens   module.TokenExpander[R]

	special func(statsCall[C], *R) (string, bool)

	preferName bool
}

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

func (e externalCommand[C, R]) run(d engine.Deps) module.RunFunc { return e.handler(d).run }

func (e externalCommand[C, R]) render(call statsCall[C], reply *R) string {
	if text, ok := specialText(e.special, call, reply); ok {
		return text
	}
	return e.tokens.Expand(orDefault(e.message(call.Cfg), e.fallback), reply)
}

func specialText[C any, R any](special func(statsCall[C], *R) (string, bool), call statsCall[C], reply *R) (string, bool) {
	if special == nil {
		return "", false
	}
	return special(call, reply)
}

func ignoreArgs(run module.RunFunc) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		return run(ctx, c, "", emit)
	}
}

func subDispatch(fallback module.RunFunc, subs map[string]module.RunFunc) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		sub, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
		if run, ok := subs[strings.ToLower(sub)]; ok {
			return run(ctx, c, rest, emit)
		}
		return fallback(ctx, c, args, emit)
	}
}

const snapshotTimeout = 10 * time.Second

type snapshotSpec[C any, R any] struct {
	provider string
	enabled  func(C) bool
	request  func(*module.Context, C, string) gossiprpc.Request
	stored   func(*R) zap.Field
}

func snapshotHandlers[C any, R any](d engine.Deps, spec snapshotSpec[C, R]) (online, offline module.EventHandler) {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return spec.onlineHandler(d, log), spec.offlineHandler(d, log)
}

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
