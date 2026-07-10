package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
)

// fortniteModuleName is the ModuleView key; the console MODULE_CATALOG entry
// and the dashboard module page use the same id.
const fortniteModuleName = "fortnite"

// fortniteCooldown is the shared per-command window; the gateway caches
// upstream replies, so this only shields chat from command spam, not the API.
const fortniteCooldown = 10 * time.Second

// Default reply templates. The broadcaster customizes them per command on the
// module page; blank falls back to these.
const (
	defaultFortniteStatsTemplate  = "{player} all time: {wins} wins in {matches} matches · {winrate}% WR · {kills} kills · {kd} K/D · solo {solowins}W / duo {duowins}W / squad {squadwins}W"
	defaultFortniteSeasonTemplate = "{player} this season: {wins} wins in {matches} matches · {winrate}% WR · {kills} kills · {kd} K/D · solo {solowins}W / duo {duowins}W / squad {squadwins}W"
	defaultFortniteStoreTemplate  = "Item Shop {date}: {items}"
)

// fortniteShopBudget caps the rendered {items} list so the chat line stays
// inside Twitch's 500-char message limit with room for the template around it.
const fortniteShopBudget = 380

// fortniteConfig is the module's dashboard configuration. Account is the
// linked account name (blank = the broadcaster's own Twitch login) and
// AccountType the platform namespace it lives in (epic/psn/xbl). The window
// is not configuration: !fnstats is always all-time and !season always the
// current season. The *Enabled toggles are stored "on"/"off" — empty means
// on, matching the alerts module's semantics — and each *Message is a
// customized template (blank = default).
type fortniteConfig struct {
	Account     string `json:"account"`
	AccountType string `json:"accountType"`

	StatsEnabled  string `json:"statsEnabled"`
	StatsMessage  string `json:"statsMessage"`
	SeasonEnabled string `json:"seasonEnabled"`
	SeasonMessage string `json:"seasonMessage"`
	StoreEnabled  string `json:"storeEnabled"`
	StoreMessage  string `json:"storeMessage"`
}

// Fortnite owns the Fortnite chat commands backed by the gateway service. It
// is a named, opt-in module (KindOptIn): off by default, enabled on the
// dashboard, where the broadcaster links a default account. Viewers can
// always target another player explicitly: "!fn Ninja".
//
// The command surface is one root with subcommands, plus the squashed forms
// as direct triggers:
//
//	!fn [player]         all-time Battle Royale stats (also !fn stats, !fnstats)
//	!fn season [player]  the current season's stats (also !fnseason)
//	!fn store            the current item-shop rotation (also !fnstore)
//
// All stats replies carry the solo/duo/squad breakdown; the gateway resolves
// the season window itself.
func Fortnite(d engine.Deps) module.Module {
	statsRun := fortniteStatsRun(d, fortniteStatsCommand{
		window:   "lifetime",
		enabled:  func(c fortniteConfig) string { return c.StatsEnabled },
		message:  func(c fortniteConfig) string { return c.StatsMessage },
		fallback: defaultFortniteStatsTemplate,
	})
	seasonRun := fortniteStatsRun(d, fortniteStatsCommand{
		window:   "season",
		enabled:  func(c fortniteConfig) string { return c.SeasonEnabled },
		message:  func(c fortniteConfig) string { return c.SeasonMessage },
		fallback: defaultFortniteSeasonTemplate,
	})
	storeRun := fortniteStoreRun(d)

	m := module.NewModule(fortniteModuleName, module.KindOptIn)
	m.Command("fn").Everyone().Cooldown(fortniteCooldown).
		Run(fortniteDispatchRun(statsRun, seasonRun, storeRun))
	m.Command("fnstats").Everyone().Cooldown(fortniteCooldown).Aliases("fortnitestats").
		Run(statsRun)
	m.Command("fnseason").Everyone().Cooldown(fortniteCooldown).
		Run(seasonRun)
	m.Command("fnstore").Everyone().Cooldown(fortniteCooldown).Aliases("itemshop", "fnshop").
		Run(storeRun)
	return m.Build()
}

// fortniteDispatchRun routes !fn's first argument word onto the subcommand
// runners: "stats"/"season"/"store" (and "shop") select one explicitly, and
// anything else — nothing, or a player name — is an all-time stats lookup, so
// "!fn Ninja" reads naturally.
func fortniteDispatchRun(statsRun, seasonRun, storeRun module.RunFunc) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		sub, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
		switch strings.ToLower(sub) {
		case "stats":
			return statsRun(ctx, c, rest, emit)
		case "season":
			return seasonRun(ctx, c, rest, emit)
		case "store", "shop":
			return storeRun(ctx, c, rest, emit)
		default:
			return statsRun(ctx, c, args, emit)
		}
	}
}

// fortniteStatsCommand names one stats command's wiring: the fixed window it
// queries and where its toggle and template live in the config blob.
type fortniteStatsCommand struct {
	window   string
	enabled  func(fortniteConfig) string
	message  func(fortniteConfig) string
	fallback string
}

// fortniteStatsTokens is the !fnstats template palette over the gateway reply.
func fortniteStatsTokens() map[string]func(*gatewayrpc.FortniteStatsReply) string {
	type reply = gatewayrpc.FortniteStatsReply
	return map[string]func(*reply) string{
		"player":       func(r *reply) string { return r.Player },
		"window":       func(r *reply) string { return r.Window },
		"wins":         func(r *reply) string { return i64(r.Overall.Wins) },
		"matches":      func(r *reply) string { return i64(r.Overall.Matches) },
		"kills":        func(r *reply) string { return i64(r.Overall.Kills) },
		"kd":           func(r *reply) string { return trimScore(r.Overall.KD) },
		"winrate":      func(r *reply) string { return trimScore(r.Overall.WinRate) },
		"solowins":     func(r *reply) string { return i64(r.Solo.Wins) },
		"solomatches":  func(r *reply) string { return i64(r.Solo.Matches) },
		"solokd":       func(r *reply) string { return trimScore(r.Solo.KD) },
		"duowins":      func(r *reply) string { return i64(r.Duo.Wins) },
		"duomatches":   func(r *reply) string { return i64(r.Duo.Matches) },
		"duokd":        func(r *reply) string { return trimScore(r.Duo.KD) },
		"squadwins":    func(r *reply) string { return i64(r.Squad.Wins) },
		"squadmatches": func(r *reply) string { return i64(r.Squad.Matches) },
		"squadkd":      func(r *reply) string { return trimScore(r.Squad.KD) },
	}
}

// fortniteStatsRun answers one stats command (!fn / !fnstats all-time,
// !fn season / !fnseason the current season) with the player's Battle Royale
// stats over cmd's fixed window. Template tokens: {player} {window} {wins}
// {matches} {kills} {kd}
// {winrate} plus the per-mode {solowins} {solomatches} {solokd} {duowins}
// {duomatches} {duokd} {squadwins} {squadmatches} {squadkd}.
func fortniteStatsRun(d engine.Deps, cmd fortniteStatsCommand) module.RunFunc {
	tokens := fortniteStatsTokens()
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg fortniteConfig
		_ = c.Decode(&cfg)
		if !alertOn(cmd.enabled(cfg)) || d.Gateway == nil {
			return nil
		}

		account := resolveAccount(accountSources{Arg: args, Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		req := gatewayrpc.Request{
			Account:     account,
			AccountType: cfg.AccountType,
			TimeWindow:  cmd.window,
			IsPremium:   c.Regress.IsPremium(),
		}
		var reply gatewayrpc.FortniteStatsReply
		if err := d.Gateway.Call(ctx, "fortnite", "stats", req, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		msg := module.ExpandString(orDefault(cmd.message(cfg), cmd.fallback), func(key string) (string, bool) {
			if field, ok := tokens[key]; ok {
				return field(&reply), true
			}
			return module.ParseDynamic(key)
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// fortniteStoreRun answers !store with the current item-shop rotation.
// Template tokens: {date} {count} {items}.
func fortniteStoreRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg fortniteConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.StoreEnabled) || d.Gateway == nil {
			return nil
		}

		var reply gatewayrpc.FortniteShopReply
		req := gatewayrpc.Request{IsPremium: c.Regress.IsPremium()}
		if err := d.Gateway.Call(ctx, "fortnite", "shop", req, &reply); err != nil {
			if chatReplyError(c, emit, "item shop", err) {
				return nil
			}
			return err
		}

		msg := module.ExpandString(orDefault(cfg.StoreMessage, defaultFortniteStoreTemplate), func(key string) (string, bool) {
			switch key {
			case "date":
				return reply.Date, true
			case "count":
				return strconv.Itoa(reply.Count), true
			case "items":
				return formatShopEntries(reply.Entries), true
			}
			return module.ParseDynamic(key)
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// formatShopEntries renders the shop offers as "Name (price), ..." within the
// chat budget; whatever does not fit collapses into "+N more". Prices are
// V-Bucks; a zero price (a free or bugged offer) renders name-only.
func formatShopEntries(entries []gatewayrpc.FortniteShopEntry) string {
	if len(entries) == 0 {
		return "empty today"
	}
	var b strings.Builder
	shown := 0
	for _, e := range entries {
		part := e.Name
		if e.Price > 0 {
			part += " (" + i64(e.Price) + ")"
		}
		if shown > 0 && b.Len()+len(part)+2 > fortniteShopBudget {
			break
		}
		if shown > 0 {
			b.WriteString(", ")
		}
		b.WriteString(part)
		shown++
	}
	if rest := len(entries) - shown; rest > 0 {
		b.WriteString(" +" + strconv.Itoa(rest) + " more")
	}
	return b.String()
}
