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

	"go.uber.org/zap"
)

const raffleModuleName = "raffle"

const raffleWinnerCooldown = 5 * time.Second

func Raffle(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(raffleModuleName, module.KindOptIn)
	m.Command("raffle").Everyone().Run(raffleDispatch(d, log))
	m.Command("sraffle").Aliases("singleraffle").Everyone().Run(raffleModOnly(d, log, raffleCmd.startSingle))
	m.Command("mraffle").Aliases("multiraffle").Everyone().Run(raffleModOnly(d, log, raffleCmd.startMulti))
	m.Command("cancelraffle").Everyone().Run(raffleModOnly(d, log, raffleCmd.cancel))
	m.Command("join").Everyone().When(raffleRunning(d)).Run(raffleStandalone(d, log, raffleCmd.join))
	m.Command("claim").Everyone().Run(raffleStandalone(d, log, raffleCmd.claimConfirm))
	m.Command("winner").Everyone().Cooldown(raffleWinnerCooldown).Run(raffleStandalone(d, log, raffleCmd.last))
	return m.Build()
}

func raffleRunning(d engine.Deps) module.ClaimFunc {
	return func(ctx context.Context, c *module.Context) bool {
		if d.Raffle == nil {
			return false
		}
		st, err := d.Raffle.Status(ctx, c.BroadcasterID)
		return err != nil || st.Open
	}
}

type raffleCmd struct {
	chatReplier
	r       engine.RaffleStore
	q       engine.QueueStore
	cfg     engine.RaffleModuleConfig
	loyalty func(context.Context) (points string, on bool)
	log     *zap.Logger
}

func newRaffleCmd(d engine.Deps, c *module.Context, log *zap.Logger) (rc raffleCmd, ok bool) {
	if d.Raffle == nil {
		return raffleCmd{}, false
	}
	rc = raffleCmd{chatReplier: newChatReplier(c), r: d.Raffle, q: d.Queue, log: log,
		loyalty: func(ctx context.Context) (string, bool) { return loyaltyVoice(ctx, d, c, "") }}
	_ = c.Decode(&rc.cfg)
	return rc, true
}

func (rc raffleCmd) entrant() engine.RaffleEntrant {
	return engine.RaffleEntrant{ID: rc.c.Env.ChatterUserID, Login: strings.ToLower(rc.c.Env.ChatterUserLogin)}
}

type raffleRunner func(rc raffleCmd, ctx context.Context, args string, emit module.Emit) error

func raffleStandalone(d engine.Deps, log *zap.Logger, fn raffleRunner) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		rc, ok := newRaffleCmd(d, c, log)
		if !ok {
			return nil
		}
		return fn(rc, ctx, args, emit)
	}
}

func raffleModOnly(d engine.Deps, log *zap.Logger, fn raffleRunner) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if !c.Chatter().Allows(module.RoleModerator) {
			return nil
		}
		rc, ok := newRaffleCmd(d, c, log)
		if !ok {
			return nil
		}
		return fn(rc, ctx, args, emit)
	}
}

type raffleRoute struct {
	mod bool
	run raffleRunner
}

var raffleRoutes = map[string]raffleRoute{
	"status": {run: raffleCmd.status},
	"draw":   {mod: true, run: raffleCmd.draw},
	"close": {mod: true, run: func(rc raffleCmd, ctx context.Context, _ string, emit module.Emit) error {
		return rc.draw(ctx, "", emit)
	}},
	"cancel": {mod: true, run: raffleCmd.cancel},
	"join":   {run: raffleCmd.join},
}

func raffleDispatch(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		rc, ok := newRaffleCmd(d, c, log)
		if !ok {
			return nil
		}
		sub, rest := splitFirst(args)
		route, known := raffleRoutes[strings.ToLower(sub)]
		isMod := c.Chatter().Allows(module.RoleModerator)
		if !known {
			return rc.unrouted(ctx, args, isMod, emit)
		}
		if route.mod && !isMod {
			return nil
		}
		return route.run(rc, ctx, rest, emit)
	}
}

func (rc raffleCmd) unrouted(ctx context.Context, args string, isMod bool, emit module.Emit) error {
	switch {
	case isMod:
		return rc.startSingle(ctx, args, emit)
	case strings.TrimSpace(args) == "":
		return rc.status(ctx, "", emit)
	}
	return nil
}

func (rc raffleCmd) status(ctx context.Context, _ string, emit module.Emit) error {
	st, err := rc.r.Status(ctx, rc.c.BroadcasterID)
	if err != nil {
		rc.log.Warn("raffle: status failed", rc.c.BID(), zap.Error(err))
		return err
	}
	switch {
	case !st.Open:
		rc.reply(emit, "", "raffle.status.closed")
	case st.SecondsLeft < 60:
		rc.reply(emit, "", "raffle.status.open.secs",
			"count", strconv.FormatInt(st.Entrants, 10),
			"secs", strconv.FormatInt(max(st.SecondsLeft, 1), 10))
	default:
		rc.reply(emit, "", "raffle.status.open",
			"count", strconv.FormatInt(st.Entrants, 10),
			"mins", strconv.FormatInt((st.SecondsLeft+59)/60, 10))
	}
	return nil
}

func (rc raffleCmd) startSingle(ctx context.Context, args string, emit module.Emit) error {
	return rc.start(ctx, raffleLaunch{args: args}, emit)
}

func (rc raffleCmd) startMulti(ctx context.Context, args string, emit module.Emit) error {
	return rc.start(ctx, raffleLaunch{args: args, multi: true}, emit)
}

type raffleLaunch struct {
	args  string
	multi bool
}

func (rc raffleCmd) start(ctx context.Context, launch raffleLaunch, emit module.Emit) error {
	req, valid := parseRaffleStart(launch.args, rc.cfg)
	if !valid {
		rc.reply(emit, "", "raffle.err.usage")
		return nil
	}
	if _, on := rc.loyalty(ctx); req.prize > 0 && !on {
		rc.reply(emit, "", "raffle.err.loyalty_off")
		return nil
	}

	opened, err := rc.r.Open(ctx, rc.c.BroadcasterID, engine.RaffleOpenSpec{
		OpenedBy: strings.ToLower(rc.c.Env.ChatterUserLogin),
		Duration: req.duration,
		Prize:    req.prize,
		Multi:    launch.multi,
	})
	if err != nil {
		rc.log.Warn("raffle: open failed", rc.c.BID(), zap.Error(err))
		return err
	}
	switch {
	case opened && rc.cfg.OpenedOn():
		rc.replyOpened(emit, req.duration)
	case opened:
	default:
		rc.reply(emit, "", "raffle.open.already")
	}
	return nil
}

func (rc raffleCmd) replyOpened(emit module.Emit, duration time.Duration) {
	secs := int64(duration.Seconds())
	key := replyKey("raffle.opened")
	if secs%60 != 0 {
		key = "raffle.opened.secs"
	}
	rc.reply(emit, rc.cfg.OpenedMessage, key,
		"mins", strconv.FormatInt((secs+59)/60, 10), "secs", strconv.FormatInt(secs, 10))
}

func (rc raffleCmd) join(ctx context.Context, _ string, emit module.Emit) error {
	entrant := rc.entrant()
	if entrant.ID == "" || entrant.Login == "" {
		return nil
	}
	entry, err := rc.r.Join(ctx, rc.c.BroadcasterID, entrant)
	if err != nil {
		rc.log.Warn("raffle: join failed", rc.c.BID(), zap.Error(err))
		return err
	}
	rc.replyJoin(ctx, emit, entry)
	return nil
}

func (rc raffleCmd) replyJoin(ctx context.Context, emit module.Emit, entry engine.RaffleEntry) {
	switch {
	case !entry.Open:
		if rc.cfg.NoRaffleOn() {
			rc.reply(emit, rc.cfg.NoRaffleMessage, "raffle.join.closed")
		}
	case rc.entryLineOn(entry):
		rc.replyEntered(ctx, emit, entry)
	}
}

func (rc raffleCmd) entryLineOn(entry engine.RaffleEntry) bool {
	if entry.Joined {
		return rc.cfg.JoinOn()
	}
	return rc.cfg.AlreadyOn()
}

func (rc raffleCmd) replyEntered(ctx context.Context, emit module.Emit, entry engine.RaffleEntry) {
	override, key := rc.cfg.AlreadyMessage, replyKey("raffle.join.already")
	if entry.Joined {
		override, key = rc.cfg.JoinMessage, "raffle.joined"
	}
	line := rc.render(override, key, "count", strconv.FormatInt(entry.Entrants, 10))
	if override == "" && rc.queueOpen(ctx) {
		line += " " + rc.render("", "raffle.join.queue_hint")
	}
	rc.say(emit, line)
}

func (rc raffleCmd) queueOpen(ctx context.Context) bool {
	if rc.q == nil || !rc.c.ModuleOn(queueModuleName) {
		return false
	}
	open, err := rc.q.IsOpen(ctx, rc.c.BroadcasterID)
	return err == nil && open
}

func (rc raffleCmd) draw(ctx context.Context, arg string, emit module.Emit) error {
	override := int64(0)
	if n, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64); err == nil && n > 0 {
		override = n
	}
	res, err := rc.r.Draw(ctx, rc.c.BroadcasterID, override)
	if err != nil {
		rc.log.Warn("raffle: draw failed", rc.c.BID(), zap.Error(err))
		return err
	}
	if res == nil {
		rc.reply(emit, "", "raffle.status.closed")
		return nil
	}
	points, _ := rc.loyalty(ctx)
	announce := engine.RaffleAnnouncement{Locale: rc.c.Locale, Config: rc.cfg, Points: points}
	for _, line := range announce.ResultLines(res) {
		rc.say(emit, line)
	}
	return nil
}

func (rc raffleCmd) cancel(ctx context.Context, _ string, emit module.Emit) error {
	ok, err := rc.r.Cancel(ctx, rc.c.BroadcasterID)
	if err != nil {
		rc.log.Warn("raffle: cancel failed", rc.c.BID(), zap.Error(err))
		return err
	}
	if ok {
		rc.reply(emit, "", "raffle.cancelled")
	} else {
		rc.reply(emit, "", "raffle.status.closed")
	}
	return nil
}

func (rc raffleCmd) last(ctx context.Context, _ string, emit module.Emit) error {
	res, found, err := rc.r.LastResult(ctx, rc.c.BroadcasterID)
	if err != nil {
		rc.log.Warn("raffle: last failed", rc.c.BID(), zap.Error(err))
		return err
	}
	switch {
	case !found:
		rc.reply(emit, "", "raffle.last.none")
	case len(res.Winners) == 0:
		rc.reply(emit, "", "raffle.last.empty")
	case len(res.Claims) > 0:
		rc.reply(emit, "", "raffle.last.confirmed",
			"targets", mentionTargets(res.Winners),
			"entrants", strconv.FormatInt(res.Entrants, 10),
			"confirmed", strconv.Itoa(len(res.Claims)),
			"total", strconv.Itoa(len(res.Winners)))
	default:
		rc.reply(emit, "", "raffle.last",
			"targets", mentionTargets(res.Winners),
			"entrants", strconv.FormatInt(res.Entrants, 10))
	}
	return nil
}

func (rc raffleCmd) claimConfirm(ctx context.Context, _ string, emit module.Emit) error {
	viewerID := rc.c.Env.ChatterUserID
	if viewerID == "" || !rc.cfg.ClaimOn() {
		return nil
	}
	outcome, err := rc.r.Claim(ctx, rc.c.BroadcasterID, viewerID)
	if err != nil {
		rc.log.Warn("raffle: claim failed", rc.c.BID(), zap.Error(err))
		return err
	}
	switch outcome {
	case engine.ClaimOk:
		if rc.cfg.ClaimOkOn() {
			rc.reply(emit, rc.cfg.ClaimOkMessage, "raffle.claim.ok")
		}
	case engine.ClaimAlready:
		rc.reply(emit, "", "raffle.claim.already")
	case engine.ClaimLate:
		rc.reply(emit, "", "raffle.claim.late")
	default:
		rc.reply(emit, "", "raffle.claim.none")
	}
	return nil
}

func mentionTargets(winners []engine.RaffleWinner) string {
	prefixed := make([]string, len(winners))
	for i, w := range winners {
		prefixed[i] = "@" + w.Login
	}
	return strings.Join(prefixed, ", ")
}
