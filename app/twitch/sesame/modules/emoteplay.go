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

const emoteplayModuleName = "emoteplay"

const maxPyramidWidth = 10

func EmotePlay(d engine.Deps) module.Module {
	m := module.NewModule(emoteplayModuleName, module.KindOptIn)
	m.On("channel.chat.message", emotePlayOnChat(d))
	return m.Build()
}

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

func logBumpFailure(c *module.Context, err error) {
	if c.Log != nil {
		c.Log.Debug("emoteplay: bump failed",
			c.BID(), zap.Error(err))
	}
}

func emotePlayAnnounce(c *module.Context, emit module.Emit, emote string, res engine.EmotePlayResult) {
	switch {
	case res.PyramidDone:
		text := module.KV(
			"user", strings.TrimPrefix(c.Env.ChatterName(), "@"),
			"emote", emote,
			"height", strconv.Itoa(res.Apex),
		).WithLocale(module.Locale(c.Locale)).ExpandString(i18n.T(c.Locale, "emoteplay.pyramid"))
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
	case res.StreakMilestone:
		text := module.KV(
			"emote", emote,
			"count", strconv.Itoa(res.Streak),
		).WithLocale(module.Locale(c.Locale)).ExpandString(i18n.T(c.Locale, "emoteplay.streak"))
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
	}
}

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

func emoteTokenish(token string) bool {
	return strings.ContainsFunc(token, emoteRuneOK)
}

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
