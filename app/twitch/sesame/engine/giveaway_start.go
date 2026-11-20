// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/codec"
)

const giveawayWindowRefusal = "giveaway.open.window"

type GiveawayStart struct {
	Channel  uint64
	Actor    draw.ViewerID
	Preset   string
	Prize    string
	Winners  int
	Window   time.Duration
	ChatArgs string
}

type GiveawayStartOutcome struct {
	Round   draw.Round
	Config  GiveawayModuleConfig
	Rules   eligibility.Rules
	Refusal string
}

func StartGiveaway(ctx context.Context, d Deps, req GiveawayStart) (GiveawayStartOutcome, error) {
	cfg, state, err := readGiveawayModule(ctx, d.Proj, req.Channel)
	if err != nil {
		return GiveawayStartOutcome{}, err
	}
	if state != ModuleOn {
		return startRefused("giveaway.open.disabled")
	}
	req = cfg.startOptions(req)
	preset, ok := cfg.LaunchPreset(req.Preset)
	if !ok {
		return startRefused("giveaway.open.preset")
	}
	if reason := validateGiveawayStart(d, req); reason != "" {
		return startRefused(reason)
	}
	if ok, reason := GiveawayOpenGate(ctx, d, req.Channel, preset.Rules); !ok {
		return startRefused("giveaway.open." + string(reason))
	}
	return openGiveaway(ctx, d, req, giveawayLaunch{config: cfg, preset: preset})
}

func startRefused(key string) (GiveawayStartOutcome, error) {
	return GiveawayStartOutcome{Refusal: key}, nil
}

func (c GiveawayModuleConfig) startOptions(req GiveawayStart) GiveawayStart {
	req.Prize = strings.TrimSpace(req.Prize)
	if req.ChatArgs != "" {
		req.Preset, req.Prize = c.parseChatStart(req.ChatArgs)
	}
	if req.Prize == "" {
		req.Prize = strings.TrimSpace(c.DefaultPrize)
	}
	if req.Winners == 0 {
		req.Winners = c.Stock()
	}
	if req.Window == 0 {
		req.Window = c.EntryWindow()
	}
	return req
}

func (c GiveawayModuleConfig) parseChatStart(args string) (preset, prize string) {
	args = strings.TrimSpace(args)
	first, rest := args, ""
	if boundary := strings.IndexFunc(args, unicode.IsSpace); boundary >= 0 {
		first, rest = args[:boundary], strings.TrimSpace(args[boundary:])
	}
	if p, ok := c.Preset(first); ok {
		return p.Name, rest
	}
	return "", args
}

func validateGiveawayStart(d Deps, req GiveawayStart) string {
	switch {
	case req.Prize == "":
		return "giveaway.open.prize"
	case utf8.RuneCountInString(req.Prize) > modulesrpc.GiveawayPrizeMaxLen:
		return "giveaway.open.prize_length"
	case giveawayPrizeBlocked(d, req.Prize):
		return "giveaway.prize_blocked"
	case req.Winners < 1 || req.Winners > draw.Pins.MaxWinners:
		return "giveaway.open.winners"
	case req.Window < draw.Pins.GiveawayMinEntryWindow || req.Window > draw.Pins.GiveawayMaxEntryWindow:
		return giveawayWindowRefusal
	default:
		return ""
	}
}

func giveawayPrizeBlocked(d Deps, prize string) bool {
	if kind, _ := moderation.MatchFloorPrescan(prize); kind != moderation.FloorNone {
		return true
	}
	return d.Automod.FloorPrescan(prize)
}

type giveawayLaunch struct {
	config GiveawayModuleConfig
	preset GiveawayPreset
}

func openGiveaway(ctx context.Context, d Deps, req GiveawayStart, launch giveawayLaunch) (GiveawayStartOutcome, error) {
	if prior, _, err := d.Draw.Joinable(ctx, draw.ChannelID(req.Channel)); err != nil || prior.Phase == draw.PhaseOpen {
		return GiveawayStartOutcome{Refusal: giveawayBusyKey(prior)}, err
	}
	open, err := giveawayOpenRequest(req, launch)
	if err != nil {
		return GiveawayStartOutcome{}, err
	}
	if err := takeGiveawaySnapshot(ctx, d.StandingsSnapshots, launch.preset, &open); err != nil {
		return giveawayOpenError(err)
	}
	round, outcome, err := d.Draw.Open(ctx, open)
	if err != nil {
		return giveawayOpenError(err)
	}
	if outcome != draw.Opened {
		return giveawayStartBusy(ctx, d.Draw, open.Channel)
	}
	return GiveawayStartOutcome{Round: round, Config: launch.config, Rules: launch.preset.Rules}, nil
}

func giveawayStartBusy(ctx context.Context, store DrawStore, channel draw.ChannelID) (GiveawayStartOutcome, error) {
	r, _, err := store.Joinable(ctx, channel)
	return GiveawayStartOutcome{Refusal: giveawayBusyKey(r)}, err
}

func giveawayOpenError(err error) (GiveawayStartOutcome, error) {
	if errors.Is(err, draw.ErrInvalidTerms) {
		return startRefused(giveawayWindowRefusal)
	}
	return GiveawayStartOutcome{}, err
}

func giveawayBusyKey(r draw.Round) string {
	if r.Terms.Kind == draw.KindGiveaway {
		return "draw.open.busy_giveaway"
	}
	return "giveaway.open.busy"
}

func giveawayOpenRequest(req GiveawayStart, launch giveawayLaunch) (draw.OpenRequest, error) {
	raw, err := codec.Marshal(launch.preset.Rules)
	terms := draw.Terms{Kind: draw.KindGiveaway, Prize: draw.Prize{Item: req.Prize},
		Winners: draw.WinnerPolicy{Count: req.Winners}, ClaimWindow: launch.config.ClaimWindow(),
		RerollNoShows: bool(launch.config.RerollNoShows), Rules: raw}
	return draw.OpenRequest{ID: draw.NewRoundID(), Channel: draw.ChannelID(req.Channel),
		Actor: req.Actor, Terms: terms, Duration: req.Window}, err
}

func takeGiveawaySnapshot(ctx context.Context, standings StandingsSnapshots, preset GiveawayPreset, req *draw.OpenRequest) error {
	if !preset.Rules.NeedsStandings() {
		return nil
	}
	closesAt := time.Now().UTC().Add(req.Duration).Truncate(time.Millisecond)
	ref, err := standings.SnapshotCreate(ctx, SnapshotRequest{
		BroadcasterID: uint64(req.Channel), Owner: "round:" + string(req.ID), ExpiresAt: closesAt,
	})
	if err != nil {
		return err
	}
	if ref.SnapshotID == "" {
		return errors.New("giveaway: missing standings snapshot")
	}
	req.Terms.SnapshotID, req.Terms.ClosesAt, req.Duration = ref.SnapshotID, closesAt, 0
	return nil
}
