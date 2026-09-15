package mail

import (
	"strings"
	"testing"
	"time"
)

func TestGiveawayTemplatePendingDoesNotInventDates(t *testing.T) {
	html, err := renderGiveawayHTML(GiveawayMessage{Months: 3, Subscriber: true, BillingPending: true}, "https://dashboard.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"3 months", "recurring subscription", "being arranged", "Billing protection is still being checked"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(html, "January 1, 0001") {
		t.Fatal("zero dates rendered")
	}
	text := giveawayText(GiveawayMessage{Months: 3, Subscriber: true, BillingPending: true}, "https://dashboard.test")
	if !strings.Contains(text, "recurring subscription") || strings.Contains(text, "next payment") {
		t.Fatalf("text=%s", text)
	}
}

func TestGiveawayTemplateConfirmedDateAndNextPayment(t *testing.T) {
	start := time.Date(2026, 9, 20, 14, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 20, 14, 0, 0, 0, time.UTC)
	next := end
	html, err := renderGiveawayHTML(GiveawayMessage{Months: 3, Start: start, End: end, NextPayment: &next}, "https://dashboard.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, detail := range []string{"September 20, 2026", "December 20, 2026", "Next scheduled payment"} {
		if !strings.Contains(html, detail) {
			t.Errorf("html missing confirmed detail: %s", detail)
		}
	}
}
