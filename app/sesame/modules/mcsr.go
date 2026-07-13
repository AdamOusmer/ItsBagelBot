package modules

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/i18n"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"

	"go.uber.org/zap"
)

// mcsrModuleName is the ModuleView key; the console MODULE_CATALOG entry and
// the dashboard module page use the same id.
const mcsrModuleName = "mcsr"

// mcsrCooldown is the shared per-command window; the gateway caches upstream
// replies (the MCSR API allows 500 requests / 10 min fleet-wide), so this only
// shields chat from spam.
const mcsrCooldown = 10 * time.Second

// mcsrSnapshotTimeout bounds the fire-and-forget stream-start snapshot call.
const mcsrSnapshotTimeout = 10 * time.Second

const (
	defaultMcsrEloTemplate     = "{player}: {elo} elo · rank #{rank} · {wins}W {losses}L this season"
	defaultMcsrSessionTemplate = "{player} this stream: {elochange} elo ({elo} now) · {wins}W {losses}L in {matches} matches"
)

// mcsrConfig is the module's dashboard configuration. Account is the linked
// default MCSR Ranked account (blank = the broadcaster's own Twitch login).
// Toggle/message semantics match the urchin module.
type mcsrConfig struct {
	Account string `json:"account"`

	EloEnabled     string `json:"eloEnabled"`
	EloMessage     string `json:"eloMessage"`
	SessionEnabled string `json:"sessionEnabled"`
	SessionMessage string `json:"sessionMessage"`
}

// Mcsr owns the MCSR Ranked commands backed by the gateway service. It is a
// named, opt-in module (KindOptIn): off by default, enabled on the dashboard
// with a linked account.
//
// Commands: !elo (current rating + season record), !session (elo and record
// since the stream started). The session baseline is snapshotted when
// stream.online arrives — the gateway stores the player's standing keyed by
// this channel — so "this stream" is exactly the live session's duration.
func Mcsr(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(mcsrModuleName, module.KindOptIn)
	m.Command("elo").Everyone().Cooldown(mcsrCooldown).Aliases("mcsr", "ranked").
		Run(mcsrEloRun(d))
	m.Command("session").Everyone().Cooldown(mcsrCooldown).Aliases("mcsrsession").
		Run(mcsrSessionRun(d))

	// Snapshot the linked account's standing the moment the stream goes online.
	// The pipeline only runs this when the module is enabled, and it wires the
	// module config in, so the snapshot targets the linked account. Fire and
	// forget on a Background-derived context (the consumer's ctx is acked and
	// may cancel the moment the handler returns), mirroring the live module's
	// write discipline.
	m.On("stream.online", func(_ context.Context, c *module.Context, _ module.Emit) error {
		if d.Gateway == nil {
			return nil
		}
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		account := resolveAccount(accountSources{Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		channelID := strconv.FormatUint(c.BroadcasterID, 10)
		go func() {
			wctx, cancel := context.WithTimeout(context.Background(), mcsrSnapshotTimeout)
			defer cancel()
			var reply gatewayrpc.McsrSnapshotReply
			if err := d.Gateway.Call(wctx, "mcsr", "session_start", gatewayrpc.Request{Account: account, ChannelID: channelID, IsPremium: c.Regress.IsPremium()}, &reply); err != nil {
				log.Warn("mcsr: stream-start snapshot failed",
					zap.String("channel_id", channelID), zap.String("account", account), zap.Error(err))
				return
			}
			log.Debug("mcsr: stream-start snapshot stored",
				zap.String("channel_id", channelID), zap.String("account", account), zap.Int("elo", reply.Elo))
		}()
		return nil
	})

	return m.Build()
}

// mcsrEloRun answers !elo with the player's current standing. Template tokens:
// {player} {elo} {rank} {wins} {losses} {matches} {country}.
func mcsrEloRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.EloEnabled) || d.Gateway == nil {
			return nil
		}

		account := resolveAccount(accountSources{Arg: args, Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		var reply gatewayrpc.McsrUserReply
		if err := d.Gateway.Call(ctx, "mcsr", "user", gatewayrpc.Request{Account: account, IsPremium: c.Regress.IsPremium()}, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		tmpl := orDefault(cfg.EloMessage, defaultMcsrEloTemplate)
		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "player":
				return reply.Nickname, true
			case "elo":
				return mcsrElo(c.Locale, reply.Elo), true
			case "rank":
				return mcsrRank(reply.Rank), true
			case "wins":
				return strconv.Itoa(reply.Wins), true
			case "losses":
				return strconv.Itoa(reply.Loses), true
			case "matches":
				return strconv.Itoa(reply.Played), true
			case "country":
				return reply.Country, true
			default:
				return module.ParseDynamic(key)
			}
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// mcsrSessionRun answers !session with the delta since the stream-start
// snapshot. Template tokens: {player} {elo} {elochange} {wins} {losses}
// {matches}. Without a baseline (module enabled mid-stream) the gateway starts
// tracking now and the reply says so instead of faking a zero delta.
func mcsrSessionRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.SessionEnabled) || d.Gateway == nil {
			return nil
		}

		// !session is always the linked account, never a typed argument: the
		// baseline snapshot is stored per channel and keyed to the linked
		// account, so honoring an arbitrary player would clobber the streamer's
		// stream-start baseline. Per-player lookups go through !elo instead.
		account := resolveAccount(accountSources{Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		req := gatewayrpc.Request{Account: account, ChannelID: strconv.FormatUint(c.BroadcasterID, 10), IsPremium: c.Regress.IsPremium()}
		var reply gatewayrpc.McsrSessionReply
		if err := d.Gateway.Call(ctx, "mcsr", "session", req, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		if !reply.HasSnapshot {
			emit(&module.Output{
				Type:          outgress.TypeChat,
				BroadcasterID: c.Env.BroadcasterUserID,
				Text:          reply.Nickname + ": " + fmt.Sprintf(i18n.T(c.Locale, "mcsr.session.started"), mcsrElo(c.Locale, reply.Elo)),
			})
			return nil
		}

		tmpl := orDefault(cfg.SessionMessage, defaultMcsrSessionTemplate)
		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "player":
				return reply.Nickname, true
			case "elo":
				return mcsrElo(c.Locale, reply.Elo), true
			case "elochange":
				return signed(reply.EloChange), true
			case "wins":
				return strconv.Itoa(reply.Wins), true
			case "losses":
				return strconv.Itoa(reply.Loses), true
			case "matches":
				return strconv.Itoa(reply.Played), true
			default:
				return module.ParseDynamic(key)
			}
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// mcsrElo renders an elo value, naming the unrated sentinel.
func mcsrElo(locale string, elo int) string {
	if elo < 0 {
		return i18n.T(locale, "mcsr.unrated")
	}
	return strconv.Itoa(elo)
}

// mcsrRank renders a leaderboard rank, dashing the unranked sentinel.
func mcsrRank(rank int) string {
	if rank < 0 {
		return "—"
	}
	return strconv.Itoa(rank)
}
