package modules

import (
	"errors"
	"strconv"
	"strings"

	"ItsBagelBot/app/sesame/i18n"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"
)

// Shared helpers for the external-stats modules (urchin, mcsr): both resolve a
// linked account the same way, chat upstream reply errors the same way, and
// format the same kinds of numbers.

// accountSources is the fallback chain resolveAccount picks from, highest
// priority first: the argument the viewer typed, the module's linked account,
// and the broadcaster's own Twitch login.
type accountSources struct {
	Arg              string
	Linked           string
	BroadcasterLogin string
}

// resolveAccount picks the account a stats command targets, in priority order:
// an explicit argument typed after the command (first word, '@' stripped), the
// module's configured linked account, then the broadcaster's own Twitch login
// (the "default linked account per user" — most streamers use the same handle).
func resolveAccount(s accountSources) string {
	if first, _, _ := strings.Cut(strings.TrimSpace(s.Arg), " "); first != "" {
		return strings.TrimPrefix(first, "@")
	}
	if linked := strings.TrimSpace(s.Linked); linked != "" {
		return linked
	}
	return s.BroadcasterLogin
}

// chatReplyError turns a gateway failure into a chat line so the viewer gets an
// answer instead of silence. A reply-level failure (player not found, rate
// limited) chats the gateway's own message and reports handled=true. An
// infrastructure failure (timeout, no responder) also chats — a cold lookup can
// outlive sesame's RPC budget while the gateway finishes and caches, so telling
// the viewer to retry is exactly right — but reports handled=false so the
// caller still propagates the error for logging.
func chatReplyError(c *module.Context, emit module.Emit, account string, err error) bool {
	var re bus.RPCReplyError
	if errors.As(err, &re) {
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          account + ": " + re.Message,
		})
		return true
	}
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          account + ": " + i18n.T(c.Locale, "external.retry"),
	})
	return false
}

// ratio renders kills/deaths style ratios with two decimals; a zero
// denominator counts as one so a flawless run shows the raw numerator.
func ratio(num, den int64) string {
	if den == 0 {
		den = 1
	}
	return strconv.FormatFloat(float64(num)/float64(den), 'f', 2, 64)
}

// signed renders a delta with an explicit sign so "gained 12" and "lost 12"
// never read the same ("+12", "-12", "±0").
func signed(n int) string {
	switch {
	case n > 0:
		return "+" + strconv.Itoa(n)
	case n < 0:
		return strconv.Itoa(n)
	default:
		return "±0"
	}
}

// orDefault returns tmpl unless it is blank, then def.
func orDefault(tmpl, def string) string {
	if strings.TrimSpace(tmpl) == "" {
		return def
	}
	return tmpl
}

// i64 renders an int64 for template tokens.
func i64(n int64) string { return strconv.FormatInt(n, 10) }

// trimScore renders a float score without trailing zero noise (7.50 -> 7.5,
// 3.00 -> 3).
func trimScore(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}
