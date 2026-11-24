// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package campaign_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/campaign"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/ent/marketingsend"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/consent"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/email"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/mac"
	"go.uber.org/zap"
)

const mailingAddress = "1 Bagel Street, Montreal"

type fakeSender struct {
	mu       sync.Mutex
	sent     []email.Message
	failures []error
}

func (s *fakeSender) Send(_ context.Context, m email.Message) (email.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, m)
	if len(s.failures) > 0 {
		err := s.failures[0]
		s.failures = s.failures[1:]
		return email.Receipt{}, err
	}
	return email.Receipt{ID: "re_" + strconv.Itoa(len(s.sent))}, nil
}

func (s *fakeSender) keys() []string {
	var keys []string
	for _, m := range s.sent {
		keys = append(keys, m.IdempotencyKey)
	}
	return keys
}

type fixture struct {
	client *ent.Client
	repo   *repository.Users
	sender *fakeSender
	svc    *campaign.Service
	sleeps []time.Duration
	onWait func()
}

func setup(t *testing.T) *fixture {
	t.Helper()
	handle, err := keyset.NewHandle(mac.HMACSHA256Tag256KeyTemplate())
	require.NoError(t, err)
	addressMAC, err := mac.New(handle)
	require.NoError(t, err)
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(repository.UsersConfig{
		Client: client, Packer: dbtest.NewPacker(t), MarketingMAC: addressMAC, Pub: bustest.NewPublisher(), Log: zap.NewNop(),
	})
	t.Cleanup(func() { repo.Close(context.Background()) })
	f := &fixture{client: client, repo: repo, sender: &fakeSender{}}
	f.svc = campaign.New(repo, f.sender, campaign.Settings{
		From: "ItsBagelBot <news@example.test>", Origin: "https://dash.example.test/", MailingAddress: mailingAddress,
		Rate: 10000, Sleep: f.sleep,
	}, zap.NewNop())
	return f
}

func (f *fixture) sleep(_ context.Context, d time.Duration) error {
	f.sleeps = append(f.sleeps, d)
	if f.onWait != nil {
		f.onWait()
	}
	return nil
}

func (f *fixture) member(t *testing.T, id uint64, locale string, marketing bool) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, f.repo.Register(ctx, repository.Registration{ID: id, Username: "user" + strconv.FormatUint(id, 10), Email: strconv.FormatUint(id, 10) + "@twitch.tv"}))
	require.NoError(t, f.repo.SetContactEmail(ctx, id, "user"+strconv.FormatUint(id, 10)+"@example.test"))
	_, err := f.repo.SetConsent(ctx, id, repository.TermsConsent{
		TermsVersion: consent.TermsVersion, Marketing: marketing, Source: consent.Consent, Locale: locale, RequestID: "req-" + strconv.FormatUint(id, 10),
	})
	require.NoError(t, err)
}

func (f *fixture) approved(t *testing.T, content map[string]consent.CampaignContent) repository.Campaign {
	t.Helper()
	draft, err := f.repo.CampaignSave(t.Context(), repository.CampaignDraft{Name: "Launch", Content: content, ActorID: "1"})
	require.NoError(t, err)
	approved, err := f.repo.CampaignApprove(t.Context(), repository.CampaignApproval{ID: draft.ID, Revision: draft.Revision, ActorID: "2"})
	require.NoError(t, err)
	return approved
}

func (f *fixture) status(t *testing.T, id int) consent.CampaignStatus {
	t.Helper()
	campaign, err := f.repo.CampaignGet(t.Context(), id)
	require.NoError(t, err)
	return campaign.Status
}

func (f *fixture) sendStatuses(t *testing.T) []marketingsend.Status {
	t.Helper()
	var out []marketingsend.Status
	for _, row := range f.client.MarketingSend.Query().AllX(t.Context()) {
		out = append(out, row.Status)
	}
	return out
}

var english = map[string]consent.CampaignContent{
	"en": {Subject: "News", HTML: "<p>Hello</p>", Text: "Hello"},
	"fr": {Subject: "Nouvelles", HTML: "<p>Salut</p>", Text: "Salut"},
}

func rejected(t *testing.T) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"name":"validation_error","message":"invalid recipient"}`))
	}))
	t.Cleanup(server.Close)
	sender := email.New(email.Config{APIKey: "key", From: "a@example.test", Endpoint: server.URL + "/emails"})
	_, err := sender.Send(t.Context(), email.Message{To: "b@example.test", Subject: "s", HTML: "<p>h</p>", Text: "t", IdempotencyKey: "k"})
	require.ErrorIs(t, err, email.ErrRejected)
	require.ErrorIs(t, err, email.ErrUnconfirmed)
	return err
}

func rateLimited(t *testing.T, retryAfter string) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", retryAfter)
		w.Header().Set("Ratelimit-Limit", "5")
		w.Header().Set("Ratelimit-Remaining", "0")
		w.Header().Set("Ratelimit-Reset", retryAfter)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"name":"rate_limit_exceeded","message":"slow down"}`))
	}))
	t.Cleanup(server.Close)
	sender := email.New(email.Config{APIKey: "key", From: "a@example.test", Endpoint: server.URL + "/emails"})
	_, err := sender.Send(t.Context(), email.Message{To: "b@example.test", Subject: "s", HTML: "<p>h</p>", Text: "t", IdempotencyKey: "k"})
	require.ErrorIs(t, err, email.ErrRateLimited)
	return err
}

func TestTickSendsLocalizedComplianceMailOnce(t *testing.T) {
	f := setup(t)
	f.member(t, 11, "fr", true)
	f.member(t, 12, "en", true)
	f.member(t, 13, "en", false)
	approved := f.approved(t, english)

	require.NoError(t, f.svc.Tick(t.Context()))
	require.NoError(t, f.svc.Tick(t.Context()))

	require.Len(t, f.sender.sent, 2)
	first := f.sender.sent[0]
	require.Equal(t, []string{"user11@example.test", "category=marketing", "campaign=" + strconv.Itoa(approved.ID)},
		[]string{first.To, "category=" + first.Tags["category"], "campaign=" + first.Tags["campaign"]})
	require.Equal(t, repository.SendKey(approved.ID, 11), first.IdempotencyKey)
	require.Equal(t, "List-Unsubscribe=One-Click", first.Headers["List-Unsubscribe-Post"])
	require.True(t, strings.HasPrefix(first.Headers["List-Unsubscribe"], "<https://dash.example.test/u/v1.11."))
	require.Contains(t, first.HTML, mailingAddress)
	require.Contains(t, first.Text, "support@itsbagelbot.com")
	require.Equal(t, consent.CampaignDone, f.status(t, approved.ID))
	require.Equal(t, []marketingsend.Status{marketingsend.StatusSent, marketingsend.StatusSent}, f.sendStatuses(t))
}

func TestLocaleFallsBackToEnglishContent(t *testing.T) {
	f := setup(t)
	f.member(t, 11, "fr", true)
	f.approved(t, map[string]consent.CampaignContent{"en": english["en"]})

	require.NoError(t, f.svc.Tick(t.Context()))

	require.Len(t, f.sender.sent, 1)
	require.Equal(t, "News", f.sender.sent[0].Subject)
}

func TestSuppressedAndPausedCampaignsSendNothing(t *testing.T) {
	f := setup(t)
	f.member(t, 11, "en", true)
	f.member(t, 12, "en", true)
	require.NoError(t, f.repo.SuppressAddress(t.Context(), "user11@example.test", consent.SuppressComplaint))
	approved := f.approved(t, english)
	_, err := f.repo.CampaignPause(t.Context(), approved.ID)
	require.NoError(t, err)

	require.NoError(t, f.svc.Tick(t.Context()))
	require.Empty(t, f.sender.sent)

	_, err = f.repo.CampaignResume(t.Context(), approved.ID)
	require.NoError(t, err)
	require.NoError(t, f.svc.Tick(t.Context()))
	require.Equal(t, []string{repository.SendKey(approved.ID, 12)}, f.sender.keys())
}

func TestRateLimitWaitsForProviderDelayThenSendsWithSameKey(t *testing.T) {
	f := setup(t)
	f.member(t, 11, "en", true)
	f.sender.failures = []error{rateLimited(t, "3")}
	approved := f.approved(t, english)

	require.NoError(t, f.svc.Tick(t.Context()))

	require.Equal(t, []time.Duration{3 * time.Second}, f.sleeps)
	require.Equal(t, []string{repository.SendKey(approved.ID, 11), repository.SendKey(approved.ID, 11)}, f.sender.keys())
	require.Equal(t, []marketingsend.Status{marketingsend.StatusSent}, f.sendStatuses(t))
}

func TestConsentWithdrawnDuringBackoffSkipsTheSend(t *testing.T) {
	f := setup(t)
	f.member(t, 11, "en", true)
	f.sender.failures = []error{rateLimited(t, "2")}
	f.onWait = func() {
		token, err := f.repo.UnsubscribeToken(t.Context(), 11)
		require.NoError(t, err)
		require.NoError(t, f.repo.SetMarketingByToken(t.Context(), token, repository.MarketingChoice{Status: consent.Withdrawn, Source: consent.OneClick}))
	}
	f.approved(t, english)

	require.NoError(t, f.svc.Tick(t.Context()))

	require.Len(t, f.sender.sent, 1)
	require.Equal(t, []marketingsend.Status{marketingsend.StatusSkipped}, f.sendStatuses(t))
}

func TestUnconfirmedSendRetriesNextTickWithoutDuplicating(t *testing.T) {
	f := setup(t)
	f.member(t, 11, "en", true)
	f.sender.failures = []error{email.ErrUnconfirmed}
	approved := f.approved(t, english)

	require.ErrorIs(t, f.svc.Tick(t.Context()), email.ErrUnconfirmed)
	require.Equal(t, []marketingsend.Status{marketingsend.StatusQueued}, f.sendStatuses(t))
	require.NoError(t, f.svc.Tick(t.Context()))

	key := repository.SendKey(approved.ID, 11)
	require.Equal(t, []string{key, key}, f.sender.keys())
	require.Equal(t, []marketingsend.Status{marketingsend.StatusSent}, f.sendStatuses(t))
}

func TestRejectedRecipientFailsAndTheCampaignContinues(t *testing.T) {
	f := setup(t)
	f.member(t, 11, "en", true)
	f.member(t, 12, "en", true)
	f.sender.failures = []error{rejected(t)}
	approved := f.approved(t, english)

	require.NoError(t, f.svc.Tick(t.Context()))

	require.Equal(t, []marketingsend.Status{marketingsend.StatusFailed, marketingsend.StatusSent}, f.sendStatuses(t))
	require.Equal(t, consent.CampaignDone, f.status(t, approved.ID))
}

func TestRepeatedUnconfirmedSendsFailTheTicketSoTheCursorAdvances(t *testing.T) {
	f := setup(t)
	f.member(t, 11, "en", true)
	f.member(t, 12, "en", true)
	f.sender.failures = []error{email.ErrUnconfirmed, email.ErrUnconfirmed, email.ErrUnconfirmed}
	approved := f.approved(t, english)

	require.ErrorIs(t, f.svc.Tick(t.Context()), email.ErrUnconfirmed)
	require.ErrorIs(t, f.svc.Tick(t.Context()), email.ErrUnconfirmed)
	require.Equal(t, []marketingsend.Status{marketingsend.StatusQueued}, f.sendStatuses(t))
	require.NoError(t, f.svc.Tick(t.Context()))

	require.Equal(t, []marketingsend.Status{marketingsend.StatusFailed, marketingsend.StatusSent}, f.sendStatuses(t))
	require.Equal(t, consent.CampaignDone, f.status(t, approved.ID))
}

func TestSendTestNeedsGrantedConsent(t *testing.T) {
	f := setup(t)
	f.member(t, 11, "en", true)
	f.member(t, 12, "en", false)
	approved := f.approved(t, english)

	require.NoError(t, f.svc.SendTest(t.Context(), approved.ID, 11))
	require.ErrorIs(t, f.svc.SendTest(t.Context(), approved.ID, 12), repository.ErrNotEligible)

	require.Len(t, f.sender.sent, 1)
	require.Equal(t, "[Test] News", f.sender.sent[0].Subject)
	require.Empty(t, f.sendStatuses(t))
}
