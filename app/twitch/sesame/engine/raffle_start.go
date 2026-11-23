// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/projection"
)

func (c RaffleModuleConfig) Terms(launch RaffleLaunch) draw.Terms {
	launch.MidCountDisabled = !c.MidCountOn()
	return RaffleTerms(launch)
}

func StartRaffle(ctx context.Context, d Deps, req draw.OpenRequest) (draw.Round, string, error) {
	on, err := LoyaltyOn(ctx, d.Proj, uint64(req.Channel))
	if err != nil {
		return draw.Round{}, "", err
	}
	if !on {
		return draw.Round{}, "loyalty_off", nil
	}
	r, result, err := d.Draw.Open(ctx, req)
	switch {
	case errors.Is(err, draw.ErrInvalidTerms):
		return draw.Round{}, "invalid", nil
	case err != nil || result == draw.Opened:
		return r, "", err
	case result == draw.JoinableBusy:
		active, _, err := d.Draw.Joinable(ctx, req.Channel)
		return draw.Round{}, raffleBusyReason(active), err
	default:
		return draw.Round{}, "busy_raffle", nil
	}
}

func raffleBusyReason(round draw.Round) string {
	if round.Terms.Kind == draw.KindGiveaway {
		return "busy_giveaway"
	}
	return "busy_raffle"
}

func (c RaffleModuleConfig) OpenedLine(ctx context.Context, proj projection.Reader, locale string, r draw.Round) string {
	a := RaffleAnnouncement{Locale: locale}
	fields := raffleOpeningFields(locale, r, DrawCurrencyName(ctx, proj, uint64(r.Channel)))
	line := a.render("", raffleOpenedKey(r.Terms), fields...)
	extra := strings.TrimSpace(a.expand(c.OpenedMessage, fields))
	if extra == "" || extra == line {
		return line
	}
	if floorHit(extra) || len(line)+len(extra)+1 > validate.MaxResponseLineLength {
		return line
	}
	return line + " " + extra
}

func raffleOpeningFields(locale string, r draw.Round, currency string) []string {
	left := r.ClosesAt.Sub(r.OpenedAt)
	return []string{"points", currency, "amount", strconv.FormatInt(r.Terms.Prize.Points, 10),
		"count", strconv.Itoa(draw.ResolveWinners(r.Terms.Winners, 0, r.Terms.Prize)),
		"time", i18n.CompactDuration(locale, left),
		"mins", strconv.FormatInt(int64((left+time.Minute-1)/time.Minute), 10),
		"secs", strconv.FormatInt(int64((left+time.Second-1)/time.Second), 10),
	}
}

func raffleOpenedKey(terms draw.Terms) string {
	switch {
	case terms.Winners.Auto:
		return "raffle.opened.auto"
	case draw.ResolveWinners(terms.Winners, 0, terms.Prize) == 1:
		return "raffle.opened"
	default:
		return "raffle.opened.split"
	}
}
