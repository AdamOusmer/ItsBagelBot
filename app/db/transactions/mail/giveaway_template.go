// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mail

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/i18n"
)

type GiveawayMessage struct {
	To             string
	Months         int
	Start          time.Time
	End            time.Time
	Subscriber     bool
	BillingPending bool
	Confirmation   bool
	NextPayment    *time.Time
	IdempotencyKey string
	Locale         string
}

type giveawayData struct {
	Subscriber   bool
	MonthsText   string
	PeriodText   string
	Situation    string
	StatusLine   string
	StatusDetail string
}

func (msg GiveawayMessage) pending() bool {
	return msg.BillingPending || msg.Start.IsZero() || msg.End.IsZero()
}

func (msg GiveawayMessage) copy() (giveawayData, error) {
	if msg.Months <= 0 {
		return giveawayData{}, errors.New("giveaway email requires a positive duration")
	}
	if msg.pending() {
		return msg.pendingCopy(), nil
	}
	return msg.confirmedCopy()
}

func (msg GiveawayMessage) pendingCopy() giveawayData {
	data := giveawayData{
		Subscriber: msg.Subscriber,
		MonthsText: localizedPrizeMonths(msg.Locale, msg.Months),
		PeriodText: i18n.T(msg.Locale, "mail.giveaway.pending_period"),
		StatusLine: i18n.T(msg.Locale, "mail.giveaway.pending_status"),
	}
	if msg.Subscriber {
		data.Situation = i18n.T(msg.Locale, "mail.giveaway.pending_situation")
	}
	return data
}

func (msg GiveawayMessage) confirmedCopy() (giveawayData, error) {
	if !msg.End.After(msg.Start) {
		return giveawayData{}, errors.New("giveaway email requires an ordered interval")
	}
	if msg.NextPayment != nil && msg.NextPayment.Before(msg.End) {
		return giveawayData{}, errors.New("next payment precedes confirmed prize end")
	}
	data := giveawayData{MonthsText: localizedPrizeMonths(msg.Locale, msg.Months), Subscriber: msg.Subscriber}
	data.PeriodText = fmt.Sprintf(i18n.T(msg.Locale, "mail.giveaway.confirmed_period"), emailDate(msg.Locale, msg.Start), emailDate(msg.Locale, msg.End))
	data.StatusLine = i18n.T(msg.Locale, "mail.giveaway.confirmed_status")
	data.StatusDetail = i18n.T(msg.Locale, "mail.giveaway.confirmed_detail")
	if msg.Subscriber {
		data.Situation = i18n.T(msg.Locale, "mail.giveaway.confirmed_situation")
	}
	if msg.NextPayment != nil {
		data.Situation += " " + fmt.Sprintf(i18n.T(msg.Locale, "mail.giveaway.next_payment"), emailDate(msg.Locale, *msg.NextPayment))
	}
	return data, nil
}

func emailDate(locale string, date time.Time) string {
	date = date.UTC()
	return strings.NewReplacer(
		"{month}", i18n.T(locale, fmt.Sprintf("mail.date.month.%d", date.Month())),
		"{day}", strconv.Itoa(date.Day()), "{year}", strconv.Itoa(date.Year()),
		"{time}", date.Format("15:04"),
	).Replace(i18n.T(locale, "mail.date.format"))
}

func giveawayEnvelope(msg GiveawayMessage, dashboardURL string) (premiumData, error) {
	copy, err := msg.copy()
	if err != nil {
		return premiumData{}, err
	}
	color := colorGreen
	if msg.pending() {
		color = colorTan
	}

	title, heading := i18n.T(msg.Locale, "mail.giveaway.title"), i18n.T(msg.Locale, "mail.giveaway.heading")
	if msg.Confirmation {
		title, heading = i18n.T(msg.Locale, "mail.giveaway.confirmed_title"), i18n.T(msg.Locale, "mail.giveaway.confirmed_heading")
	}
	chrome := mailChrome(msg.Locale)
	return premiumData{
		Title: title, Preheader: fmt.Sprintf(i18n.T(msg.Locale, "mail.giveaway.text_intro"), copy.MonthsText),
		Category: i18n.T(msg.Locale, "mail.giveaway.category"), Heading: heading,
		StatusLine: copy.StatusLine, StatusDetail: copy.StatusDetail, StatusColor: color,
		PerkPeriod: i18n.T(msg.Locale, "mail.giveaway.perk"), ActionLabel: i18n.T(msg.Locale, "mail.giveaway.action"),
		Footer:       i18n.T(msg.Locale, "mail.giveaway.footer"),
		DashboardURL: strings.TrimRight(dashboardURL, "/") + "/billing", Giveaway: &copy, Locale: msg.Locale,
		GiftIntro: chrome["mail.gift.intro"], GiftNoStrings: chrome["mail.gift.no_strings"],
		PriorityLabel: chrome["mail.priority.label"], PriorityBody: chrome["mail.priority.body"],
		PerksLabel: chrome["mail.perks.label"], BetaLabel: chrome["mail.beta.label"], BetaBody: chrome["mail.beta.body"],
		SubscriptionLabel: chrome["mail.subscription"], SafetyLabel: chrome["mail.safety.label"], SafetyBody: chrome["mail.safety.body"],
		GiveawayIntro: i18n.T(msg.Locale, "mail.giveaway.intro"), Signature: i18n.T(msg.Locale, "mail.signature"),
		ProductPeriod: fmt.Sprintf(i18n.T(msg.Locale, "mail.product_period"), copy.MonthsText),
	}, nil
}

func localizedPrizeMonths(locale string, months int) string {
	if months == 1 {
		return "1 " + i18n.T(locale, "mail.month.one")
	}
	return fmt.Sprintf(i18n.T(locale, "mail.month.many"), months)
}

func renderGiveawayHTML(msg GiveawayMessage, dashboardURL string) (string, error) {
	data, err := giveawayEnvelope(msg, dashboardURL)
	if err != nil {
		return "", err
	}
	return renderPremium(data)
}

func giveawayText(msg GiveawayMessage, dashboardURL string) string {
	data, err := giveawayEnvelope(msg, dashboardURL)
	if err != nil {
		return ""
	}
	intro := fmt.Sprintf(i18n.T(msg.Locale, "mail.giveaway.text_intro"), data.Giveaway.MonthsText)
	return fmt.Sprintf("%s\n\n%s\n%s\n\n%s\n%s\n\n%s\n\n%s: %s\n\n%s %s\nDiscord: %s\n\n%s\nhttps://itsbagelbot.com\n",
		intro, data.Giveaway.PeriodText, data.Giveaway.Situation,
		data.StatusLine, data.StatusDetail, i18n.T(msg.Locale, "mail.beta.label")+" "+i18n.T(msg.Locale, "mail.beta.body"), i18n.T(msg.Locale, "mail.giveaway.action"), data.DashboardURL, i18n.T(msg.Locale, "mail.safety.label"), i18n.T(msg.Locale, "mail.safety.body")+" Discord.", discordURL, data.Footer)
}
