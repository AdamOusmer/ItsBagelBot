// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mail

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// GiveawayMessage contains only the facts approved for this delivery. Pending
// messages omit dates even if an unconfirmed plan has already been prepared.
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
		MonthsText: prizeMonthsText(msg.Months),
		PeriodText: "Your prize period is being arranged; confirmed dates will appear in your dashboard.",
		StatusLine: "Your prize is recorded and awaiting confirmation",
	}
	if msg.Subscriber {
		data.Situation = "You already have a recurring subscription. Billing protection is still being checked, so your current renewal may still be charged. Your full prize remains owed."
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
	data := giveawayData{MonthsText: prizeMonthsText(msg.Months), Subscriber: msg.Subscriber}
	data.PeriodText = "Your prize starts " + emailDate(msg.Start) + " and ends " + emailDate(msg.End) + "."
	data.StatusLine = "Your Premium prize period is confirmed"
	data.StatusDetail = "Premium activates automatically when your confirmed prize period starts."
	if msg.Subscriber {
		data.Situation = "You already have a recurring subscription. Your existing paid access is preserved, and renewal protection is confirmed for the full prize period."
	}
	if msg.NextPayment != nil {
		data.Situation += " Next scheduled payment: " + emailDate(*msg.NextPayment) + "."
	}
	return data, nil
}

func emailDate(date time.Time) string {
	return date.UTC().Format("January 2, 2006 at 15:04 UTC")
}

func prizeMonthsText(months int) string {
	if months == 1 {
		return "1 month"
	}
	return fmt.Sprintf("%d months", months)
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
	title, heading := "You won Premium", "You won Premium."
	if msg.Confirmation {
		title, heading = "Your Premium prize is confirmed", "Your prize is confirmed."
	}
	return premiumData{
		Title: title, Preheader: "You won " + copy.MonthsText + " of ItsBagelBot Premium.",
		Category: "Giveaway", Heading: heading,
		StatusLine: copy.StatusLine, StatusDetail: copy.StatusDetail, StatusColor: color,
		PerkPeriod: "Available throughout your confirmed prize period.", ActionLabel: "View your prize",
		Footer:       "Sent by ItsBagelBot because you won a Premium giveaway.",
		DashboardURL: strings.TrimRight(dashboardURL, "/") + "/billing", Giveaway: &copy,
	}, nil
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
	return fmt.Sprintf("You won %s of ItsBagelBot Premium.\n\n%s\n%s\n\n%s\n%s\n\n%s\n\nView your prize: %s\n\nStaying safe. %s\nDiscord: %s\n\n%s\nhttps://itsbagelbot.com\n",
		data.Giveaway.MonthsText, data.Giveaway.PeriodText, data.Giveaway.Situation,
		data.StatusLine, data.StatusDetail, betaAccessText, data.DashboardURL, safetyNotice, discordURL, data.Footer)
}
