// Command stage drives the legacy worker pipeline and the new sesame engine with
// the same fake chat/raid envelopes on both lanes and the same fake Valkey
// (in-memory custom commands + module toggles), then prints their published
// outgress messages side by side and flags MATCH / DIFF. It is a visual, runnable
// version of the compare parity test — no real NATS or Valkey.
//
//	go run ./app/sesame/cmd/stage
package main

import (
	"fmt"
	"os"
	"strings"

	"ItsBagelBot/app/sesame/compare"
	"ItsBagelBot/internal/projection"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
)

type scenario struct {
	name          string
	msg           *message.Message
	reader        compare.Reader
	special       string
	live          bool
	greet         bool
	timeSensitive bool // ping carries a live uptime; normalize it before comparing
	divergent     bool // an intentional worker/sesame difference, not a failure
	note          string
}

func main() {
	// The fake Valkey: a broadcaster's custom command set (name -> definition),
	// exactly what the projector would serve the pipeline from Valkey.
	cmds := map[string]projection.Command{
		"hi":    {Name: "hi", Response: "hello {sender}!", IsActive: true, Perm: "everyone"},
		"ann":   {Name: "ann", Response: "/announce {sender}: {args}", IsActive: true, Perm: "everyone"},
		"annb":  {Name: "annb", Response: "/announceblue {args}", IsActive: true, Perm: "everyone"},
		"so":    {Name: "so", Response: "/shoutout @{touser}", IsActive: true, Perm: "everyone"},
		"clear": {Name: "clear", Response: "/announce chat cleared", IsActive: true, Perm: "mod"},
		"lurk":  {Name: "lurk", Aliases: []string{"afk"}, Response: "{sender} is now lurking", IsActive: true, Perm: "everyone"},
	}
	cmdReader := compare.Reader{Cmds: cmds}
	shoutoutOn := compare.Reader{Mods: []projection.ModuleView{{Name: "shoutout", IsEnabled: true}}}

	scenarios := []scenario{
		{name: "baked !ping", msg: compare.ChatMsg("!ping", "9", "premium"), timeSensitive: true},
		{name: "baked !itsbagelbot", msg: compare.ChatMsg("!itsbagelbot", "9", "standard")},
		{name: "baked !source", msg: compare.ChatMsg("!source", "9", "premium")},
		{name: "plain chat (no output)", msg: compare.ChatMsg("hello everyone how are you", "9", "standard")},

		{name: "custom !hi (everyone)", msg: compare.ChatMsg("!hi", "9", "standard"), reader: cmdReader},
		{name: "custom !ann -> /announce", msg: compare.ChatMsg("!ann hello world", "9", "premium"), reader: cmdReader},
		{name: "custom !annb -> /announceblue", msg: compare.ChatMsg("!annb cold news", "9", "standard"), reader: cmdReader},
		{name: "custom !so -> /shoutout", msg: compare.ChatMsg("!so @cooldude", "9", "premium"), reader: cmdReader},
		{name: "custom alias !afk -> lurk", msg: compare.ChatMsg("!afk", "9", "standard"), reader: cmdReader},
		{name: "custom !clear (mod-only) as viewer", msg: compare.ChatMsg("!clear", "9", "standard"), reader: cmdReader, note: "gated: viewer lacks mod"},
		{name: "custom !clear (mod-only) as mod", msg: compare.ChatMsg("!clear", "9", "standard", "moderator"), reader: cmdReader},

		{name: "bagel greet (special, live)", msg: compare.ChatMsg("hi chat", "1", "premium"), special: "1", live: true, greet: true},
		{name: "bagel skipped (offline)", msg: compare.ChatMsg("hi chat", "1", "standard"), special: "1", live: false, greet: true},

		{name: "raid -> shoutout (enabled)", msg: compare.RaidMsg("standard"), reader: shoutoutOn},

		{name: "baked !announce (mod)", msg: compare.ChatMsg("!announce hi", "9", "standard", "moderator"), divergent: true,
			note: "worker: baked command; sesame: announce is middleware (no baked cmd)"},
	}

	printHeader(cmds)

	pass, fail, diverge := 0, 0, 0
	for i, sc := range scenarios {
		w, s := compare.ProcessBoth(sc.msg, sc.reader, sc.special, sc.live, sc.greet)
		ok := equalCaptures(w, s, sc.timeSensitive)

		status := "✅ MATCH"
		switch {
		case sc.divergent:
			status = "⚠️  DIVERGENT (intentional)"
			diverge++
		case ok:
			pass++
		default:
			status = "❌ DIFF"
			fail++
		}

		fmt.Printf("▸ #%02d  %-34s %s\n", i+1, sc.name, laneOf(sc.msg))
		fmt.Printf("    in    : %s\n", inputOf(sc.msg))
		fmt.Printf("    worker: %s\n", render(w))
		fmt.Printf("    sesame: %s\n", render(s))
		if sc.note != "" {
			fmt.Printf("    note  : %s\n", sc.note)
		}
		fmt.Printf("    %s\n\n", status)
	}

	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf(" %d MATCH   %d DIFF   %d intentional divergence\n", pass, fail, diverge)
	fmt.Println(strings.Repeat("─", 70))
	if fail > 0 {
		os.Exit(1)
	}
}

func printHeader(cmds map[string]projection.Command) {
	fmt.Println(strings.Repeat("═", 70))
	fmt.Println(" SESAME vs WORKER — staged side-by-side")
	fmt.Println(" fake lanes (premium/standard) + fake Valkey (in-memory custom commands)")
	fmt.Println(strings.Repeat("═", 70))
	fmt.Println(" seeded custom commands (the fake Valkey):")
	for _, name := range []string{"hi", "ann", "annb", "so", "lurk", "clear"} {
		c := cmds[name]
		perm := c.Perm
		if perm == "" {
			perm = "everyone"
		}
		alias := ""
		if len(c.Aliases) > 0 {
			alias = "  (alias: !" + strings.Join(c.Aliases, ", !") + ")"
		}
		fmt.Printf("   !%-7s [%-8s] %q%s\n", c.Name, perm, c.Response, alias)
	}
	fmt.Println(strings.Repeat("─", 70))
}

// render turns a pipeline's published messages into one readable line.
func render(cs []compare.Captured) string {
	if len(cs) == 0 {
		return "(nothing)"
	}
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, one(c))
	}
	return strings.Join(parts, "  +  ")
}

func one(c compare.Captured) string {
	lane := laneName(c.Subject)
	switch c.Msg.Type {
	case "announce":
		return fmt.Sprintf("[%s] announce color=%s ch=%s %q", lane, c.Msg.Color, c.Msg.BroadcasterID, payloadMessage(c.Msg.Payload))
	case "shoutout":
		return fmt.Sprintf("[%s] shoutout to=%s ch=%s", lane, c.Msg.To, c.Msg.BroadcasterID)
	default: // chat and everything else
		return fmt.Sprintf("[%s] %s ch=%s %q", lane, c.Msg.Type, c.Msg.BroadcasterID, payloadMessage(c.Msg.Payload))
	}
}

// equalCaptures compares two published sets; when timeSensitive it normalizes the
// ping uptime tail so the two are comparable.
func equalCaptures(w, s []compare.Captured, timeSensitive bool) bool {
	if len(w) != len(s) {
		return false
	}
	for i := range w {
		if canon(w[i], timeSensitive) != canon(s[i], timeSensitive) {
			return false
		}
	}
	return true
}

func canon(c compare.Captured, timeSensitive bool) string {
	msg := payloadMessage(c.Msg.Payload)
	if timeSensitive {
		if i := strings.Index(msg, "up for "); i >= 0 {
			msg = msg[:i+len("up for ")] + "<uptime>"
		}
	}
	return strings.Join([]string{c.Subject, c.Msg.Type, c.Msg.BroadcasterID, c.Msg.Color, c.Msg.To, msg}, "|")
}

func payloadMessage(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	var inner struct {
		Message string `json:"message"`
	}
	_ = sonic.Unmarshal(payload, &inner)
	return inner.Message
}

// --- envelope readback for printing ---

func laneOf(msg *message.Message) string  { return "lane=" + envField(msg, "lane") }
func inputOf(msg *message.Message) string  { return describeInput(msg) }
func laneName(subject string) string {
	switch subject {
	case compare.PremiumSubj:
		return "premium"
	case compare.StandardSubj:
		return "standard"
	default:
		return subject
	}
}

func describeInput(msg *message.Message) string {
	typ := envField(msg, "type")
	if typ == "channel.raid" {
		return "channel.raid  raider=CoolStreamer viewers=42"
	}
	text := envField(msg, "text")
	chatter := envField(msg, "chatter_user_id")
	badges := envBadges(msg)
	if badges != "" {
		badges = "  badges=" + badges
	}
	return fmt.Sprintf("%q  chatter=%s%s", text, chatter, badges)
}

func envField(msg *message.Message, key string) string {
	var m map[string]any
	if err := sonic.Unmarshal(msg.Payload, &m); err != nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func envBadges(msg *message.Message) string {
	var m struct {
		Badges []struct {
			SetID string `json:"set_id"`
		} `json:"badges"`
	}
	if err := sonic.Unmarshal(msg.Payload, &m); err != nil {
		return ""
	}
	ids := make([]string, 0, len(m.Badges))
	for _, b := range m.Badges {
		ids = append(ids, b.SetID)
	}
	return strings.Join(ids, ",")
}
