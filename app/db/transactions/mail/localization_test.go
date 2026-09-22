package mail

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFrenchGiftCopiesPreservePersonalData(t *testing.T) {
	for _, giver := range []string{"", "Gift"} {
		t.Run(giver, func(t *testing.T) {
			note := "Someone just made your day. <b>Gift</b>"
			body, err := giftHTMLLocale(giver, note, "https://dashboard.example", "fr")
			require.NoError(t, err)
			plain := giftTextLocale(giver, note, "https://dashboard.example", "fr")
			require.Contains(t, body, `lang="fr"`)
			require.Contains(t, body, "Someone just made your day. &lt;b&gt;Gift&lt;/b&gt;")
			require.Contains(t, plain, note)
			for _, text := range []string{body, plain} {
				for _, expected := range []string{"1 mois de Premium ItsBagelBot", "Restez en sécurité.", "@itsbagelbot.com"} {
					require.Contains(t, text, expected)
				}
				for _, unwanted := range []string{"%!(", "%!s", " gifted you", " wrote", " of ItsBagelBot Premium", "See what's new", "Sent by ItsBagelBot"} {
					require.NotContains(t, text, unwanted)
				}
			}
			if giver != "" {
				require.Contains(t, plain, "Gift a écrit")
			} else {
				require.Contains(t, plain, "Vous avez reçu")
			}
		})
	}
}

func TestFrenchGiveawayAllBillingStates(t *testing.T) {
	start := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 2, 0)
	for _, pending := range []bool{false, true} {
		msg := GiveawayMessage{Months: 2, Locale: "fr", Start: start, End: end, Subscriber: true, BillingPending: pending, NextPayment: &end}
		mailer := &Mailer{dashboardURL: "https://dashboard.example"}
		content, err := mailer.PrepareGiveaway(msg)
		require.NoError(t, err)
		require.Contains(t, content.Subject, "2 mois")
		for _, body := range []string{content.HTML, content.Text} {
			require.Contains(t, body, "2 mois")
			require.Contains(t, body, "abonnement récurrent")
			for _, english := range []string{"2 months", "September", "November", "recurring subscription", "Next scheduled payment", "View your prize", "Staying safe", " of ItsBagelBot Premium"} {
				require.NotContains(t, body, english)
			}
			if pending {
				require.Contains(t, body, "peut donc être débité")
				require.NotContains(t, body, "15 septembre")
			} else {
				require.Contains(t, body, "15 septembre 2026 à 12:00 UTC")
				require.Contains(t, body, "Prochain paiement prévu")
				require.Contains(t, body, "15 novembre 2026 à 12:00 UTC")
			}
		}
	}
}

func TestUnknownMailLocaleFallsBackToEnglish(t *testing.T) {
	body, err := giftHTMLLocale("", "", "https://dashboard.example", "unknown")
	require.NoError(t, err)
	require.Contains(t, body, `lang="en"`)
	require.Contains(t, body, "1 month of ItsBagelBot Premium")
	require.NotContains(t, body, "mail.gift.")
	require.NotContains(t, body, "mail.safety.")
}
