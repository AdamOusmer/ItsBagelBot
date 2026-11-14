// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mail

import (
	"context"
	"testing"

	"github.com/resend/resend-go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrantEmailContent(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  GrantMessage
		want expectation
	}{
		{
			name: "english names the end date and the team",
			msg:  GrantMessage{ExpiresAt: sept20},
			want: expectation{
				subject: "from the ItsBagelBot team",
				both:    []string{"The ItsBagelBot team gave you", "Your Premium runs until September 20, 2026 at 14:00 UTC.", "Staying safe."},
				html:    []string{"A little something from us.", "From the team", dashboardURL},
				text:    []string{"See what's new: " + dashboardURL},
				notBoth: []string{"%!(", "mail.grant.", "gifted you", "1 month"},
			},
		},
		{
			name: "recipient locale",
			msg:  GrantMessage{ExpiresAt: sept20, Locale: "fr"},
			want: expectation{
				subject: "L'équipe ItsBagelBot",
				html:    []string{`lang="fr"`, "Une petite attention de notre part."},
				both:    []string{"20 septembre 2026"},
				notBoth: []string{"%!(", "mail.grant.", "runs until"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := &recordingSender{reply: &resend.SendEmailResponse{Id: "grant-1"}}
			tc.msg.To, tc.msg.IdempotencyKey = "winner@example.com", "grant-key"
			require.NoError(t, (&Mailer{sender: sender, dashboardURL: dashboardURL}).SendGrant(context.Background(), tc.msg))
			assert.Equal(t, "grant-key", sender.key)
			tc.want.check(t, rendered{subject: sender.request.Subject, html: sender.request.Html, text: sender.request.Text})
		})
	}
}

func TestGrantEmailRefusals(t *testing.T) {
	var disabled *Mailer
	require.ErrorIs(t, disabled.SendGrant(context.Background(), GrantMessage{To: "winner@example.com", ExpiresAt: sept20, IdempotencyKey: "k"}), ErrDisabled)

	sender := &recordingSender{reply: &resend.SendEmailResponse{Id: "grant-1"}}
	err := (&Mailer{sender: sender}).SendGrant(context.Background(), GrantMessage{To: "winner@example.com", IdempotencyKey: "k"})
	require.ErrorIs(t, err, ErrInvalidMessage)
	assert.Zero(t, sender.calls)
}
