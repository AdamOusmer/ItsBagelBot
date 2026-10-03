// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/mail"
	"ItsBagelBot/app/db/transactions/tebex"
	users "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"

	entsql "entgo.io/ent/dialect/sql"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

type fakeUsers struct {
	mu                sync.Mutex
	prepares, commits int
	lastPrepare       users.PreparePremiumGrantRequest
	coverage          users.PremiumCoverage
	address           string
	commitErr         error
}

func (f *fakeUsers) Pool(context.Context, *time.Time) (users.GiveawayPoolReply, error) {
	return users.GiveawayPoolReply{}, nil
}

func (f *fakeUsers) Coverage(context.Context, uint64) (users.PremiumCoverage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.coverage, nil
}

func (f *fakeUsers) Prepare(_ context.Context, req users.PreparePremiumGrantRequest) (users.PremiumGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepares++
	f.lastPrepare = req
	return users.PremiumGrant{ID: 7}, nil
}

func (f *fakeUsers) Commit(context.Context, users.CommitPremiumGrantRequest) (users.PremiumGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits++
	if f.commitErr != nil {
		return users.PremiumGrant{}, f.commitErr
	}
	return users.PremiumGrant{ID: 7}, nil
}

func (f *fakeUsers) Email(context.Context, uint64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.address, nil
}

type grantLedger struct {
	prepares, commits int
	prepared          users.PreparePremiumGrantRequest
}

func (f *fakeUsers) ledger() grantLedger {
	f.mu.Lock()
	defer f.mu.Unlock()
	return grantLedger{prepares: f.prepares, commits: f.commits, prepared: f.lastPrepare}
}

type fakeProvider struct {
	payment tebex.RecurringPayment
	calls   int
}

func (p *fakeProvider) GetRecurring(context.Context, string) (tebex.RecurringPayment, error) {
	p.calls++
	return p.payment, nil
}

func (p *fakeProvider) PauseRecurring(context.Context, string, time.Time) (tebex.RecurringPayment, error) {
	return p.payment, nil
}

type ledgerMailer struct {
	templates []mail.GiveawayMessage
	sent      []mail.Delivery
	err       error
}

func (m *ledgerMailer) PrepareGiveaway(message mail.GiveawayMessage) (mail.PreparedContent, error) {
	m.templates = append(m.templates, message)
	return mail.PreparedContent{From: "from@example.com", Subject: "Prize", HTML: "<p>Prize</p>", Text: "Prize"}, nil
}

func (m *ledgerMailer) DeliverPrepared(_ context.Context, delivery mail.Delivery) (mail.Receipt, error) {
	m.sent = append(m.sent, delivery)
	return mail.Receipt{ProviderID: "receipt-1"}, m.err
}

var fixtureNow = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

type fixture struct {
	t        *testing.T
	db       *ent.Client
	users    *fakeUsers
	provider *fakeProvider
	mailer   *ledgerMailer
	config   Config
	now      time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool, err := sql.Open(testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	db := ent.NewClient(ent.Driver(entsql.OpenDB(testdb.Driver, pool)))
	require.NoError(t, db.Schema.Create(t.Context()))
	t.Cleanup(func() { _ = db.Close() })
	return &fixture{t: t, db: db, now: fixtureNow}
}

func (f *fixture) engine() *Engine {
	cfg := EngineConfig{Store: NewStore(f.db), Config: f.config, Now: func() time.Time { return f.now }}
	if f.users != nil {
		cfg.Users = f.users
	}
	if f.provider != nil {
		cfg.Provider = f.provider
	}
	if f.mailer != nil {
		cfg.Mailer = f.mailer
	}
	return NewEngine(cfg)
}

func (f *fixture) newAward(id string) *ent.GiveawayAwardCreate {
	return f.db.GiveawayAward.Create().SetID(id).SetGiveawayID("campaign").SetDrawID("draw").SetUserID(42).SetOrdinal(1).SetPrizeMonths(1).SetIntervalRule("provider-monthly-unverified").SetSelectedAt(f.now)
}

func (f *fixture) award(id string) *ent.GiveawayAward {
	f.t.Helper()
	award, err := f.newAward(id).Save(f.t.Context())
	require.NoError(f.t, err)
	return award
}

func (f *fixture) queue(award *ent.GiveawayAward, eventType string) {
	f.t.Helper()
	err := f.db.GiveawayOutbox.Create().SetID(award.ID + ":" + eventType).SetAggregateID(award.ID).SetEventType(eventType).SetPayloadJSON(`{"award_id":"` + award.ID + `"}`).
		OnConflictColumns("id").UpdateNewValues().Update(func(u *ent.GiveawayOutboxUpsert) {
		u.SetState("queued").ClearNextAttemptAt().ClearLeaseUntil().SetLeaseOwner("")
	}).Exec(f.t.Context())
	require.NoError(f.t, err)
}

func (f *fixture) dispatch(award *ent.GiveawayAward, eventType string) error {
	f.t.Helper()
	f.queue(award, eventType)
	return f.engine().DispatchOnce(f.t.Context())
}

func (f *fixture) runUntil(done func() bool) {
	f.t.Helper()
	ctx, cancel := context.WithCancel(f.t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = f.engine().Run(ctx)
	}()
	require.Eventually(f.t, done, 5*time.Second, 5*time.Millisecond)
	cancel()
	<-stopped
}
