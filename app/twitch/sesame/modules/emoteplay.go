// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// emoteplayModuleName is the ModuleView key; it matches the console
// MODULE_CATALOG entry id (console/shared/lib/types.ts).
const emoteplayModuleName = "emoteplay"

// maxPyramidWidth caps how wide a line the tracker will even look at. A line
// wider than this is ignored WITHOUT touching valkey and without resetting any
// chain: 50-token copypasta walls are not pyramid material, and letting them
// clear state would hand every troll a one-line kill switch against an active
// pyramid. Abandoned chains expire through the store's windows instead.
const maxPyramidWidth = 10

// EmotePlay celebrates chat-built emote structures: pyramids (the same emote
// stacked 1, 2, 3… and back down to 1) and streaks (a run of single-emote
// messages). It is a named opt-in module (KindOptIn): unlike the commands, it
// speaks without being asked, so broadcasters switch it on deliberately from
// its dashboard module page.
//
// Speed contract: ordinary prose never reaches valkey. The handler screens each
// line in-process with a zero-allocation shape scan and only a line that IS one
// repeated token pays the single atomic store round trip that advances both
// chains. Announcements fire only on milestones, so the steady-state cost of an
// idle channel is a few byte compares per message.
//
// Cross-replica correctness lives entirely in the store (see ValkeyEmotePlay):
// sesame's replicas share one lane consumer, so consecutive lines of a pyramid
// routinely arrive at different pods, and the store's single Lua round trip is
// what makes the second pod see the first pod's step instead of racing it.
func EmotePlay(d engine.Deps) module.Module {
	m := module.NewModule(emoteplayModuleName, module.KindOptIn)
	m.On("channel.chat.message", emotePlayOnChat(d))
	return m.Build()
}

// emotePlayOnChat feeds candidate lines to the store and announces milestones.
// A store error is swallowed at Debug level rather than returned on purpose:
// returning it would make the pipeline log at Error and notice New Relic for
// every candidate line during a valkey outage, and emote-spam channels would
// turn one cache hiccup into an alert storm. This store is best-effort hype
// state, not money — the line is lost, chat moves on, and the Debug sink is
// there if anyone goes looking.
func emotePlayOnChat(d engine.Deps) module.EventHandler {
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		if d.EmotePlay == nil || c.Env.Text == "" {
			return nil
		}
		emote, width, ok := emoteShape(c.Env.Text)
		if !ok {
			return nil
		}
		copies := len(c.Env.Senders)
		if copies < 1 {
			copies = 1
		}
		res, err := d.EmotePlay.Bump(ctx, engine.EmotePlayUpdate{
			BroadcasterID: c.BroadcasterID,
			MsgID:         c.Env.MsgID,
			Emote:         emote,
			Width:         width,
			Copies:        copies,
		})
		if err != nil {
			logBumpFailure(c, err)
			return nil
		}
		emotePlayAnnounce(c, emit, emote, res)
		return nil
	}
}

// logBumpFailure keeps the handler branch-light: a valkey outage during emote
// spam must not grow the handler's shape, so the Debug sink lives here.
func logBumpFailure(c *module.Context, err error) {
	if c.Log != nil {
		c.Log.Debug("emoteplay: bump failed",
			zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
	}
}

// emotePlayAnnounce renders the milestone replies. When one line lands both a
// completion and a streak rung (the final width-1 descent line also counts as a
// streak message), only the pyramid speaks: two overlapping celebrations of the
// same line read as noise, and the streak's next rung is never far away.
func emotePlayAnnounce(c *module.Context, emit module.Emit, emote string, res engine.EmotePlayResult) {
	switch {
	case res.PyramidDone:
		text := module.ExpandString(i18n.T(c.Locale, "emoteplay.pyramid"), func(key string) (string, bool) {
			switch key {
			case "user":
				return strings.TrimPrefix(c.Env.ChatterName(), "@"), true
			case "emote":
				return emote, true
			case "height":
				return strconv.Itoa(res.Apex), true
			default:
				return module.ParseDynamic(key)
			}
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
	case res.StreakMilestone:
		text := module.ExpandString(i18n.T(c.Locale, "emoteplay.streak"), func(key string) (string, bool) {
			switch key {
			case "emote":
				return emote, true
			case "count":
				return strconv.Itoa(res.Streak), true
			default:
				return module.ParseDynamic(key)
			}
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
	}
}

// tokenWalker walks text one whitespace-delimited token at a time, returning ""
// once exhausted. Byte-index arithmetic keeps the walk allocation-free: this is
// the hot path behind every chat line the module screens, so it must stay
// cheaper than the valkey call it gates.
type tokenWalker struct {
	text string
	i    int
}

func (w *tokenWalker) next() string {
	start := skipSpaces(w.text, w.i)
	end := skipNonSpaces(w.text, start)
	w.i = end
	return w.text[start:end]
}

// emoteShape reports whether text is exactly one token repeated (width >= 1),
// ASCII-whitespace separated, and returns the token and its count.
//
// Deliberate limits, chosen for the hot path:
//   - Case-sensitive comparison. Kappa and kappa render different emotes, and
//     mixing them mid-pyramid should break the shape, not blend it.
//   - Lines wider than maxPyramidWidth are rejected here (not by the store), so
//     copypasta walls never produce a valkey call at all.
func emoteShape(text string) (token string, width int, ok bool) {
	w := tokenWalker{text: text}
	token = w.next()
	if token == "" {
		return "", 0, false
	}
	for width = 1; width <= maxPyramidWidth; width++ {
		switch next := w.next(); {
		case next == "":
			if !emoteTokenish(token) {
				return "", 0, false
			}
			return token, width, true
		case next != token:
			return "", 0, false
		}
	}
	return "", 0, false
}

// skipSpaces advances past ASCII whitespace; skipNonSpaces past everything
// else. The pair is the whole tokenizer.
//
// Deliberate limits: ASCII whitespace only. Twitch chat separates tokens with
// plain spaces in practice; exotic U+00A0-style separators just fail the shape
// check and cost nothing, which beats paying rune-decoding on every line to
// handle them.
func skipSpaces(text string, i int) int {
	for i < len(text) && isASCIISpace(text[i]) {
		i++
	}
	return i
}

func skipNonSpaces(text string, i int) int {
	for i < len(text) && !isASCIISpace(text[i]) {
		i++
	}
	return i
}

// emoteTokenish requires at least one letter or digit, so punctuation spam
// ("...", "???") never registers as an emote. Any non-ASCII rune counts: emote
// codes exist in CJK scripts, and decoding one short token is cheap.
func emoteTokenish(token string) bool {
	return strings.ContainsFunc(token, emoteRuneOK)
}

// emoteRuneOK is emoteTokenish's predicate, kept operator-free: each acceptance
// reason is its own plain branch.
func emoteRuneOK(r rune) bool {
	if r >= utf8.RuneSelf {
		return true
	}
	if unicode.IsLetter(r) {
		return true
	}
	return unicode.IsDigit(r)
}

func isASCIISpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}
