// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"go.uber.org/zap"
)

const quotesModuleName = "quotes"

const quotesReadCooldown = 5 * time.Second

var quoteOpeners = [...]string{`"`, "“", "'"}

var quoteCloser = map[string]string{`"`: `"`, "“": "”", "'": "'"}

type quotesConfig struct {
	AddPerm  string `json:"addPerm"`
	EditPerm string `json:"editPerm"`
}

func Quotes(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(quotesModuleName, module.KindOptIn)
	m.Command("quote").Everyone().Aliases("quotes").Run(quoteDispatch(d, log))
	m.Command("quoteadd").Everyone().Aliases("addquote").Run(quoteAddCommand(d, log))
	return m.Build()
}

func quoteDispatch(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if d.Quotes == nil {
			return nil
		}
		qc := newQuotesCmd(d, c, log)
		return qc.route(ctx, strings.TrimSpace(args), c.Chatter(), emit)
	}
}

func quoteAddCommand(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if d.Quotes == nil {
			return nil
		}
		qc := newQuotesCmd(d, c, log)
		body, _ := unquote(strings.TrimSpace(args))
		return qc.runIf(c.Chatter().Allows(qc.addRole), func() error { return qc.add(ctx, body, emit) })
	}
}

func newQuotesCmd(d engine.Deps, c *module.Context, log *zap.Logger) quotesCmd {
	var cfg quotesConfig
	_ = c.Decode(&cfg)
	return quotesCmd{
		chatReplier: newChatReplier(c),
		q:           d.Quotes,
		cd:          d.Cooldown,
		addRole:     quotePermRole(cfg.AddPerm),
		editRole:    quotePermRole(cfg.EditPerm),
		log:         log,
	}
}

func quotePermRole(perm string) module.Role {
	if perm == "" {
		return module.RoleModerator
	}
	return module.ParsePerm(perm)
}

func (qc quotesCmd) route(ctx context.Context, args string, chatter module.Role, emit module.Emit) error {
	if body, ok := unquote(args); ok {
		return qc.runIf(chatter.Allows(qc.addRole), func() error { return qc.add(ctx, body, emit) })
	}

	sub, rest := splitFirst(args)
	switch strings.ToLower(sub) {
	case "", "random":
		return qc.random(ctx, emit)
	case "add":
		body, _ := unquote(rest)
		return qc.runIf(chatter.Allows(qc.addRole), func() error { return qc.add(ctx, body, emit) })
	case "edit":
		return qc.runIf(chatter.Allows(qc.editRole), func() error { return qc.edit(ctx, rest, emit) })
	case "remove", "delete":
		return qc.runIf(chatter.Allows(module.RoleModerator), func() error { return qc.remove(ctx, rest, emit) })
	default:
		if n, err := strconv.ParseUint(sub, 10, 64); err == nil && rest == "" {
			return qc.get(ctx, n, emit)
		}
		return qc.search(ctx, args, emit)
	}
}

func (qc quotesCmd) runIf(allowed bool, fn func() error) error {
	if !allowed {
		return nil
	}
	return fn()
}

func unquote(s string) (body string, ok bool) {
	for _, open := range quoteOpeners {
		if !strings.HasPrefix(s, open) {
			continue
		}
		body = strings.TrimPrefix(s, open)
		body = strings.TrimSuffix(body, quoteCloser[open])
		return strings.TrimSpace(body), true
	}
	return strings.TrimSpace(s), false
}

type quotesCmd struct {
	chatReplier
	q        engine.QuotesStore
	cd       engine.CooldownStore
	addRole  module.Role
	editRole module.Role
	log      *zap.Logger
}

func (qc quotesCmd) add(ctx context.Context, body string, emit module.Emit) error {
	if body == "" {
		qc.reply(emit, "", "quote.err.usage")
		return nil
	}
	saved, err := qc.q.QuoteAdd(ctx, qc.c.BroadcasterID, body, strings.ToLower(qc.c.Env.ChatterUserLogin))
	if err != nil {
		qc.log.Warn("quotes: add failed", qc.c.BID(), zap.Error(err))
		return err
	}
	qc.reply(emit, "", "quote.added", "num", strconv.FormatUint(saved.Number, 10))
	return nil
}

type quoteRead struct {
	fetch   func(context.Context) (modulesrpc.Quote, bool, error)
	missKey replyKey
	missKV  []string
	label   string
}

func (qc quotesCmd) get(ctx context.Context, number uint64, emit module.Emit) error {
	return qc.readAndShow(ctx, emit, quoteRead{
		fetch: func(ctx context.Context) (modulesrpc.Quote, bool, error) {
			return qc.q.QuoteGet(ctx, qc.c.BroadcasterID, number)
		},
		missKey: "quote.not_found",
		missKV:  []string{"num", strconv.FormatUint(number, 10)},
		label:   "get",
	})
}

func (qc quotesCmd) search(ctx context.Context, term string, emit module.Emit) error {
	return qc.readAndShow(ctx, emit, quoteRead{
		fetch: func(ctx context.Context) (modulesrpc.Quote, bool, error) {
			return qc.q.QuoteSearch(ctx, qc.c.BroadcasterID, term)
		},
		missKey: "quote.search.none",
		missKV:  []string{"term", term},
		label:   "search",
	})
}

func (qc quotesCmd) random(ctx context.Context, emit module.Emit) error {
	return qc.readAndShow(ctx, emit, quoteRead{
		fetch: func(ctx context.Context) (modulesrpc.Quote, bool, error) {
			return qc.q.QuoteRandom(ctx, qc.c.BroadcasterID)
		},
		missKey: "quote.none",
		label:   "random",
	})
}

func (qc quotesCmd) readAndShow(ctx context.Context, emit module.Emit, r quoteRead) error {
	if ok, err := qc.allowRead(ctx); err != nil || !ok {
		return err
	}
	quote, found, err := r.fetch(ctx)
	if err != nil {
		qc.log.Warn("quotes: "+r.label+" failed", qc.c.BID(), zap.Error(err))
		return err
	}
	if !found {
		qc.reply(emit, "", r.missKey, r.missKV...)
		return nil
	}
	qc.show(emit, quote)
	return nil
}

func (qc quotesCmd) edit(ctx context.Context, args string, emit module.Emit) error {
	target, rest := splitFirst(args)
	number, err := strconv.ParseUint(target, 10, 64)
	body, _ := unquote(rest)
	if err != nil || body == "" {
		qc.reply(emit, "", "quote.edit.usage")
		return nil
	}
	_, found, err := qc.q.QuoteEdit(ctx, qc.c.BroadcasterID, number, body)
	if err != nil {
		qc.log.Warn("quotes: edit failed", zap.Uint64("number", number), qc.c.BID(), zap.Error(err))
		return err
	}
	key := replyKey("quote.edited")
	if !found {
		key = "quote.not_found"
	}
	qc.reply(emit, "", key, "num", strconv.FormatUint(number, 10))
	return nil
}

func (qc quotesCmd) remove(ctx context.Context, args string, emit module.Emit) error {
	target, _ := splitFirst(args)
	number, err := strconv.ParseUint(target, 10, 64)
	if err != nil {
		qc.reply(emit, "", "quote.remove.usage")
		return nil
	}
	found, err := qc.q.QuoteRemove(ctx, qc.c.BroadcasterID, number)
	if err != nil {
		qc.log.Warn("quotes: remove failed", zap.Uint64("number", number), qc.c.BID(), zap.Error(err))
		return err
	}
	key := replyKey("quote.removed")
	if !found {
		key = "quote.not_found"
	}
	qc.reply(emit, "", key, "num", strconv.FormatUint(number, 10))
	return nil
}

func (qc quotesCmd) allowRead(ctx context.Context) (bool, error) {
	if qc.cd == nil {
		return true, nil
	}
	return qc.cd.Allow(ctx, engine.CommandCooldownKey(qc.c.BroadcasterID, "quote"), quotesReadCooldown)
}

func (qc quotesCmd) show(emit module.Emit, quote modulesrpc.Quote) {
	date := ""
	if t, err := time.Parse(time.RFC3339, quote.CreatedAt); err == nil {
		date = t.UTC().Format("2006-01-02")
	}
	qc.reply(emit, "", "quote.show",
		"num", strconv.FormatUint(quote.Number, 10),
		"text", quote.Text,
		"date", date,
	)
}
