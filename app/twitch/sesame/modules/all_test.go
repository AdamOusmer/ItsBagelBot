// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"maps"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func allRegistry(t *testing.T) (*engine.Registry, []module.Module) {
	t.Helper()
	d := engine.Deps{
		Special: engine.NewSpecialSet(""),
		Live:    &fakeLive{},
		Greet:   &fakeGreet{},
	}
	mods := All(d)
	require.NotEmpty(t, mods)
	return engine.NewRegistry(zap.NewNop(), mods...), mods
}

func TestAllBuildsAndIndexes(t *testing.T) {
	reg, _ := allRegistry(t)

	_, ok := reg.Command("ping")
	assert.True(t, ok)
	for _, event := range []string{"channel.chat.message", "channel.raid", "stream.online"} {
		assert.NotEmpty(t, reg.For(event), event)
	}
}

type commandSpec struct {
	name     string
	aliases  []string
	perm     module.Role
	liveOnly bool
}

func command(name string, perm module.Role, aliases ...string) commandSpec {
	return commandSpec{name: name, perm: perm, aliases: aliases}
}

func (c commandSpec) live() commandSpec {
	c.liveOnly = true
	return c
}

type moduleSpec struct {
	kind     module.Kind
	commands []commandSpec
}

func shippedModules(mods []module.Module) map[string]moduleSpec {
	shipped := make(map[string]moduleSpec, len(mods))
	for _, m := range mods {
		key := m.Name
		if key == "" && len(m.Commands) > 0 {
			key = "~" + m.Commands[0].Name
		}
		if key == "" {
			continue
		}
		spec := moduleSpec{kind: m.Kind}
		for _, c := range m.Commands {
			spec.commands = append(spec.commands, commandSpec{c.Name, c.Aliases, c.Perm, c.LiveOnly})
		}
		shipped[key] = spec
	}
	return shipped
}

const (
	roleEveryone      = module.RoleEveryone
	roleModerator     = module.RoleModerator
	roleLeadModerator = module.RoleLeadModerator
)

var variableOnlyModules = []string{"stream", "timers", "counters", "discord", "accountage", "followage", "clip", "title", "game", "tags", "commercial", "marker", "uptime"}

var shippedContract = map[string]moduleSpec{
	"~ping": {module.KindCore, []commandSpec{
		command("ping", roleEveryone), command("itsbagelbot", roleEveryone), command("source", roleEveryone),
	}},
	"personality": {module.KindDefault, []commandSpec{
		command("bagels", roleEveryone, "fed", "bagelcount"), command("bagelboard", roleEveryone, "feedboard", "bagellb"),
	}},
	"~cmd": {module.KindCore, []commandSpec{
		command("cmd", roleEveryone, "cmds", "command", "commands"),
		command("title", roleLeadModerator, "settitle"), command("game", roleLeadModerator, "setgame"), command("tags", roleLeadModerator, "settags"),
		command("commercial", roleLeadModerator, "ad").live(), command("marker", roleLeadModerator).live(),
	}},
	"~clip":         {module.KindCore, []commandSpec{command("clip", roleEveryone).live()}},
	"~followage":    {module.KindCore, []commandSpec{command("followage", roleEveryone), command("accountage", roleEveryone)}},
	"~uptime":       {module.KindCore, []commandSpec{command("uptime", roleEveryone)}},
	"shoutout":      {module.KindOptIn, nil},
	"alerts":        {module.KindDefault, nil},
	"automod":       {module.KindDefault, nil},
	"channelpoints": {module.KindOptIn, nil},
	"govee":         {module.KindOptIn, nil},
	"triggers":      {module.KindOptIn, nil},
	"emoteplay":     {module.KindOptIn, nil},
	"moderation":    {module.KindDefault, []commandSpec{command("nuke", roleModerator)}},
	"urchin": {module.KindOptIn, []commandSpec{
		command("daily", roleEveryone, "bwdaily"), command("weekly", roleEveryone, "bwweekly"), command("monthly", roleEveryone, "bwmonthly"),
		command("bwstats", roleEveryone, "bedwars"), command("sniper", roleEveryone, "urchin"),
		command("tag", roleEveryone, "tags", "bwtags"), command("tagdescription", roleEveryone),
	}},
	"mcsr": {module.KindOptIn, []commandSpec{
		command("elo", roleEveryone, "mcsr", "ranked"), command("session", roleEveryone, "mcsrsession"),
		command("lastmatch", roleEveryone, "rankedmatch"), command("record", roleEveryone, "matchrecord"),
		command("lb", roleEveryone, "leaderboard", "rankedlb"), command("race", roleEveryone, "weeklyrace"),
		command("pb", roleEveryone, "personalbest"), command("pace", roleEveryone, "pacesession", "splits"),
		command("nethers", roleEveryone, "nph"), command("lastfort", roleEveryone, "lastpace", "previousfort"),
	}},
	"fortnite": {module.KindOptIn, []commandSpec{
		command("fn", roleEveryone), command("fnstats", roleEveryone, "fortnitestats"), command("fnseason", roleEveryone),
		command("fnsession", roleEveryone), command("fnstore", roleEveryone, "itemshop", "fnshop"),
	}},
	"codm": {module.KindOptIn, []commandSpec{command("codm", roleEveryone, "codmprofile", "codmrank")}},
	"clashroyale": {module.KindOptIn, []commandSpec{
		command("cr", roleEveryone), command("crstats", roleEveryone, "clashroyale"), command("crdecks", roleEveryone, "crdeck"),
		command("crranked", roleEveryone, "crpol"), command("crroad", roleEveryone, "crtrophy"),
	}},
	"valorant": {module.KindOptIn, []commandSpec{
		command("val", roleEveryone), command("valrank", roleEveryone), command("valmatches", roleEveryone, "valhistory"),
		command("valaccount", roleEveryone, "valwho"), command("vallb", roleEveryone, "valleaderboard"),
		command("valshop", roleEveryone, "valrotation"),
	}},
	"raffle": {module.KindOptIn, []commandSpec{
		command("raffle", roleEveryone), command("join", roleEveryone), command("claim", roleEveryone), command("winner", roleEveryone),
	}},
	"queue": {module.KindOptIn, []commandSpec{
		command("queue", roleEveryone), command("join", roleEveryone), command("leave", roleEveryone), command("list", roleEveryone, "queuelist"),
	}},
	"quotes": {module.KindOptIn, []commandSpec{command("quote", roleEveryone, "quotes"), command("quoteadd", roleEveryone, "addquote")}},
	"loyalty": {module.KindOptIn, []commandSpec{
		command("points", roleEveryone), command("watchtime", roleEveryone), command("leaderboard", roleEveryone), command("counter", roleModerator),
	}},
	"gamble": {module.KindOptIn, []commandSpec{command("gamble", roleEveryone)}},
	"duel":   {module.KindOptIn, []commandSpec{command("duel", roleEveryone)}},
	"time":   {module.KindOptIn, []commandSpec{command("time", roleEveryone)}},
	"songqueue": {module.KindOptIn, []commandSpec{
		command("sr", roleEveryone, "songrequest", "songreq"), command("song", roleEveryone, "current", "nowplaying", "np"),
		command("skip", roleModerator, "next"), command("clear", roleModerator), command("remove", roleEveryone), command("srlist", roleEveryone, "songlist"),
	}},
	"trial": {module.KindCore, []commandSpec{
		command("ign", roleEveryone), command("event", roleEveryone), command("discord", roleEveryone), command("drop", roleEveryone, "drops"),
		command("cape", roleEveryone), command("socials", roleEveryone), command("twitter", roleEveryone, "x"),
		command("youtube", roleEveryone, "yt"), command("tiktok", roleEveryone), command("instagram", roleEveryone, "ig"),
		command("schedule", roleEveryone), command("merch", roleEveryone), command("sens", roleEveryone, "settings", "crosshair"),
		command("specs", roleEveryone, "setup", "pc"), command("prime", roleEveryone, "sub"), command("lurk", roleEveryone), command("unlurk", roleEveryone),
	}},
}

func TestShippedModuleContract(t *testing.T) {
	want := maps.Clone(shippedContract)
	for _, name := range variableOnlyModules {
		want[name] = moduleSpec{kind: module.KindDefault}
	}

	_, mods := allRegistry(t)
	assert.Equal(t, want, shippedModules(mods))
}

func TestRaffleOwnsStandaloneJoinOverQueue(t *testing.T) {
	reg, _ := allRegistry(t)
	bound, ok := reg.Command("join")
	require.True(t, ok)
	assert.Equal(t, raffleModuleName, bound.Owner.Name, "raffle must register before queue to own !join")
}
