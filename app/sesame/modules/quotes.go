package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"go.uber.org/zap"
)

// quotesModuleName is the ModuleView key; the console MODULE_CATALOG entry
// uses the same id.
const quotesModuleName = "quotes"

// quotesReadCooldown throttles the chat-facing readout per channel (random and
// numbered lookups share one window). The moderator mutations stay uncooled —
// they are rare and a throttled "add" that saved but never confirmed would
// look like a failure.
const quotesReadCooldown = 5 * time.Second

// quoteOpeners are the characters that mark "!quote <text>" as a save: the
// straight and curly double quotes plus the straight single quote, so mobile
// keyboards that autocorrect to smart quotes still work.
var quoteOpeners = [...]string{`"`, "“", "'"}

// quoteCloser maps each opener to the closer stripped from the end when the
// broadcaster typed a matching pair.
var quoteCloser = map[string]string{`"`: `"`, "“": "”", "'": "'"}

// quotesConfig is the broadcaster's customizable quotes settings, decoded from
// the module's ModuleView blob.
type quotesConfig struct {
	// AddPerm is the minimum role allowed to save a quote: one of the perm
	// strings module.ParsePerm understands ("everyone", "sub", "vip", "mod").
	// Empty keeps the historical moderator-only default. Removing a quote is
	// always moderator-only and is not configurable.
	AddPerm string `json:"addPerm"`
}

// Quotes owns the channel quote book. It is a named, opt-in module
// (KindOptIn): off by default, enabled on the dashboard. The rows live in the
// modules service; sesame talks to them over the quote RPC verbs.
//
//	!quote                → a random saved quote
//	!quote 12             → quote #12
//	!quote "text"         → save a new quote (also !quote add <text>)
//	!quote remove 12      → mod: delete quote #12 (also !quote delete 12)
//
// A readout is "Quote #N: text (YYYY-MM-DD)" — the save date rides at the end.
// Numbers are per channel and assigned in order; removing a quote leaves a
// hole so old numbers never point at different text. Saving is gated on the
// broadcaster's configured AddPerm (moderator by default); removing is always
// moderator-only. A gated verb typed by an insufficient role is silently
// ignored, matching the engine gate.
func Quotes(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(quotesModuleName, module.KindOptIn)
	m.Command("quote").Everyone().Aliases("quotes").Run(quoteDispatch(d, log))
	return m.Build()
}

// quoteDispatch handles !quote and routes its forms. The engine's command gate
// runs it for everyone; the mutating forms re-check the chatter's role against
// the broadcaster's config (save) or the fixed moderator floor (remove).
func quoteDispatch(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if d.Quotes == nil {
			return nil
		}
		var cfg quotesConfig
		_ = c.Decode(&cfg)
		qc := quotesCmd{q: d.Quotes, c: c, cd: d.Cooldown, addRole: quoteAddRole(cfg.AddPerm), log: log}
		return qc.route(ctx, strings.TrimSpace(args), c.Chatter(), emit)
	}
}

// quoteAddRole resolves the configured minimum role for saving a quote. Empty
// (unconfigured) keeps the historical moderator default.
func quoteAddRole(perm string) module.Role {
	if perm == "" {
		return module.RoleModerator
	}
	return module.ParsePerm(perm)
}

// route dispatches one !quote invocation. Each gated form is wrapped in runIf
// against the role it requires, so the switch stays a flat list of one-line
// routes (an insufficient role is silently ignored, matching the engine gate).
func (qc quotesCmd) route(ctx context.Context, args string, chatter module.Role, emit module.Emit) error {
	// A quoted body is the save form, checked before the subcommand split so a
	// quote that happens to start with "add ..." or a number is kept whole.
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
	case "remove", "delete":
		return qc.runIf(chatter.Allows(module.RoleModerator), func() error { return qc.remove(ctx, rest, emit) })
	default:
		if n, err := strconv.ParseUint(sub, 10, 64); err == nil && rest == "" {
			return qc.get(ctx, n, emit)
		}
		qc.reply(emit, "quote.err.usage")
		return nil
	}
}

// runIf runs fn only when allowed; otherwise it is a silent no-op.
func (qc quotesCmd) runIf(allowed bool, fn func() error) error {
	if !allowed {
		return nil
	}
	return fn()
}

// unquote reports whether s is a quoted body ("text", “text”, 'text') and
// returns the text with the surrounding pair stripped. A missing closer is
// tolerated: the opener alone marks intent and only the opener is stripped.
// Without an opener, s is returned as-is with ok=false.
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

// quotesCmd bundles the per-invocation state the handlers share.
type quotesCmd struct {
	q       engine.QuotesStore
	c       *module.Context
	cd      engine.CooldownStore
	addRole module.Role
	log     *zap.Logger
}

// add saves the quote and confirms with its assigned number.
func (qc quotesCmd) add(ctx context.Context, body string, emit module.Emit) error {
	if body == "" {
		qc.reply(emit, "quote.err.usage")
		return nil
	}
	saved, err := qc.q.QuoteAdd(ctx, qc.c.BroadcasterID, body, strings.ToLower(qc.c.Env.ChatterUserLogin))
	if err != nil {
		qc.log.Warn("quotes: add failed", qc.bid(), zap.Error(err))
		return err
	}
	qc.reply(emit, "quote.added", "num", strconv.FormatUint(saved.Number, 10))
	return nil
}

// quoteRead is one cooled read: how to fetch the quote, the reply for a miss,
// and a label for the failure log. get and random differ only in these.
type quoteRead struct {
	fetch   func(context.Context) (modulesrpc.Quote, bool, error)
	missKey string
	missKV  []string
	label   string
}

// get shows one numbered quote, behind the shared read cooldown.
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

// random shows a random quote, behind the shared read cooldown.
func (qc quotesCmd) random(ctx context.Context, emit module.Emit) error {
	return qc.readAndShow(ctx, emit, quoteRead{
		fetch: func(ctx context.Context) (modulesrpc.Quote, bool, error) {
			return qc.q.QuoteRandom(ctx, qc.c.BroadcasterID)
		},
		missKey: "quote.none",
		label:   "random",
	})
}

// readAndShow gates on the shared read cooldown, runs r.fetch, and emits either
// the readout or r's miss reply. A nil/throttled cooldown stays silent.
func (qc quotesCmd) readAndShow(ctx context.Context, emit module.Emit, r quoteRead) error {
	if ok, err := qc.allowRead(ctx); err != nil || !ok {
		return err
	}
	quote, found, err := r.fetch(ctx)
	if err != nil {
		qc.log.Warn("quotes: "+r.label+" failed", qc.bid(), zap.Error(err))
		return err
	}
	if !found {
		qc.reply(emit, r.missKey, r.missKV...)
		return nil
	}
	qc.show(emit, quote)
	return nil
}

// remove deletes one numbered quote.
func (qc quotesCmd) remove(ctx context.Context, args string, emit module.Emit) error {
	target, _ := splitFirst(args)
	number, err := strconv.ParseUint(target, 10, 64)
	if err != nil {
		qc.reply(emit, "quote.remove.usage")
		return nil
	}
	found, err := qc.q.QuoteRemove(ctx, qc.c.BroadcasterID, number)
	if err != nil {
		qc.log.Warn("quotes: remove failed", zap.Uint64("number", number), qc.bid(), zap.Error(err))
		return err
	}
	key := "quote.removed"
	if !found {
		key = "quote.not_found"
	}
	qc.reply(emit, key, "num", strconv.FormatUint(number, 10))
	return nil
}

// allowRead claims the shared per-channel read window. A nil store (tests)
// runs unthrottled; a throttled or errored claim stays silent, matching the
// engine gate.
func (qc quotesCmd) allowRead(ctx context.Context) (bool, error) {
	if qc.cd == nil {
		return true, nil
	}
	return qc.cd.Allow(ctx, engine.CommandCooldownKey(qc.c.BroadcasterID, "quote"), quotesReadCooldown)
}

// show emits the readout: the text followed by the save date. The date is the
// day the quote was added, rendered as YYYY-MM-DD (UTC).
func (qc quotesCmd) show(emit module.Emit, quote modulesrpc.Quote) {
	date := ""
	if t, err := time.Parse(time.RFC3339, quote.CreatedAt); err == nil {
		date = t.UTC().Format("2006-01-02")
	}
	qc.reply(emit, "quote.show",
		"num", strconv.FormatUint(quote.Number, 10),
		"text", quote.Text,
		"date", date,
	)
}

// reply emits one localized chat line. kv are {token},value pairs; {user} (the
// invoking chatter) is always available.
func (qc quotesCmd) reply(emit module.Emit, key string, kv ...string) {
	text := module.ExpandString(i18n.T(qc.c.Locale, key), func(k string) (string, bool) {
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
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: qc.c.Env.BroadcasterUserID,
		Text:          text,
	})
}

// bid is the broadcaster-id log field, shared by every handler's warn path.
func (qc quotesCmd) bid() zap.Field { return zap.Uint64("broadcaster_id", qc.c.BroadcasterID) }
