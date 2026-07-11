package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/i18n"
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

// Quotes owns the channel quote book. It is a named, opt-in module
// (KindOptIn): off by default, enabled on the dashboard. The rows live in the
// commands service; sesame talks to them over the quote RPC verbs.
//
//	!quote                → a random saved quote
//	!quote 12             → quote #12
//	!quote "text"         → mod: save a new quote (also !quote add <text>)
//	!quote remove 12      → mod: delete quote #12 (also !quote delete 12)
//
// A readout is "Quote #N: text (YYYY-MM-DD)" — the save date rides at the end.
// Numbers are per channel and assigned in order; removing a quote leaves a
// hole so old numbers never point at different text. Moderator verbs typed by
// a non-mod are silently ignored, matching the engine gate's silence on an
// insufficient role.
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
// runs it for everyone; the moderator-only mutations re-check the role.
func quoteDispatch(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if d.Quotes == nil {
			return nil
		}
		qc := quotesCmd{q: d.Quotes, c: c, log: log}
		args = strings.TrimSpace(args)
		isMod := c.Chatter().Allows(module.RoleModerator)

		// A quoted body is the save form, checked before the subcommand split so
		// a quote that happens to start with "add ..." or a number is kept whole.
		if body, ok := unquote(args); ok {
			if !isMod {
				return nil
			}
			return qc.add(ctx, body, emit)
		}

		sub, rest := splitFirst(args)
		switch strings.ToLower(sub) {
		case "":
			return qc.random(ctx, d.Cooldown, emit)
		case "random":
			return qc.random(ctx, d.Cooldown, emit)
		case "add":
			if !isMod {
				return nil
			}
			body, _ := unquote(rest)
			return qc.add(ctx, body, emit)
		case "remove", "delete":
			if !isMod {
				return nil
			}
			return qc.remove(ctx, rest, emit)
		default:
			if n, err := strconv.ParseUint(sub, 10, 64); err == nil && rest == "" {
				return qc.get(ctx, n, d.Cooldown, emit)
			}
			qc.reply(emit, "quote.err.usage")
			return nil
		}
	}
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
	q   engine.QuotesStore
	c   *module.Context
	log *zap.Logger
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

// get shows one numbered quote, behind the shared read cooldown.
func (qc quotesCmd) get(ctx context.Context, number uint64, cd engine.CooldownStore, emit module.Emit) error {
	if ok, err := qc.allowRead(ctx, cd); err != nil || !ok {
		return err
	}
	quote, found, err := qc.q.QuoteGet(ctx, qc.c.BroadcasterID, number)
	if err != nil {
		qc.log.Warn("quotes: get failed", zap.Uint64("number", number), qc.bid(), zap.Error(err))
		return err
	}
	if !found {
		qc.reply(emit, "quote.not_found", "num", strconv.FormatUint(number, 10))
		return nil
	}
	qc.show(emit, quote)
	return nil
}

// random shows a random quote, behind the shared read cooldown.
func (qc quotesCmd) random(ctx context.Context, cd engine.CooldownStore, emit module.Emit) error {
	if ok, err := qc.allowRead(ctx, cd); err != nil || !ok {
		return err
	}
	quote, found, err := qc.q.QuoteRandom(ctx, qc.c.BroadcasterID)
	if err != nil {
		qc.log.Warn("quotes: random failed", qc.bid(), zap.Error(err))
		return err
	}
	if !found {
		qc.reply(emit, "quote.none")
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
func (qc quotesCmd) allowRead(ctx context.Context, cd engine.CooldownStore) (bool, error) {
	if cd == nil {
		return true, nil
	}
	return cd.Allow(ctx, engine.CommandCooldownKey(qc.c.BroadcasterID, "quote"), quotesReadCooldown)
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
