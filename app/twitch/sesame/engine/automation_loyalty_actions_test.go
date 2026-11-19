// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"

	loyaltyent "ItsBagelBot/app/db/loyalty/ent"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/automation"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type sqliteSpendStore struct {
	LoyaltyStore
	repo *loyaltyrepo.Loyalty
}

func (s sqliteSpendStore) BalanceSpend(ctx context.Context, spend PointSpend) (loyaltyrpc.Balance, bool, bool, error) {
	row, found, spent, err := s.repo.BalanceSpendViewer(ctx, loyaltyrepo.Spend{
		UserID: spend.BroadcasterID, ViewerID: spend.ViewerID, ViewerLogin: spend.Login, Amount: spend.Amount,
	})
	balance := loyaltyrpc.Balance{}
	if row != nil {
		balance.Points = row.Points
	}
	return balance, found, spent, err
}

func newSQLiteSpendStore(t *testing.T, balance int64) sqliteSpendStore {
	t.Helper()
	driver := entsql.OpenDB("sqlite3", completionSQL(t, "sqlite3", "spend"))
	client := loyaltyent.NewClient(loyaltyent.Driver(driver))
	require.NoError(t, client.Schema.Create(t.Context()))
	_, err := client.Balance.Create().SetUserID(123).SetViewerID(999).SetViewerLogin("alice").SetPoints(balance).Save(t.Context())
	require.NoError(t, err)
	return sqliteSpendStore{repo: loyaltyrepo.NewLoyalty(client, driver, nil, zap.NewNop())}
}

func TestProcessPointsSpendUsesAtomicBalanceAndMatchingBranch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		balance int64
		runs    int
		left    int64
		lines   []string
	}{
		{name: "enough", balance: 100, runs: 1, left: 40, lines: []string{"spent 40"}},
		{name: "insufficient", balance: 50, runs: 1, left: 50, lines: []string{"refused 50"}},
		{name: "parallel", balance: 100, runs: 2, left: 40, lines: []string{"spent 40", "refused 40"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newSQLiteSpendStore(t, tc.balance)
			lines := processSpends(t, store, tc.runs)
			row, _, err := store.repo.BalanceGet(t.Context(), 123, 999)
			require.NoError(t, err)
			assert.Equal(t, tc.left, row.Points)
			assert.ElementsMatch(t, tc.lines, lines)
		})
	}
}

func processSpends(t *testing.T, store sqliteSpendStore, count int) []string {
	t.Helper()
	auto := command("!spend", obj{})
	auto.Definition.Actions = []automation.Action{act("points_spend", automation.PointsSpendAction{
		Amount:    60,
		OnOK:      []automation.Action{act("chat", obj{"text": "spent {points}"})},
		OnFailure: []automation.Action{act("chat", obj{"text": "refused {points}"})},
	})}
	pub := &fakePublisher{}
	var runs sync.WaitGroup
	for range count {
		p, _ := (autoRig{automations: []automation.Automation{auto}, loyalty: store, pub: pub}).pipeline()
		runs.Go(func() { process(t, p, chatEnv("!spend", "")) })
	}
	runs.Wait()
	var lines []string
	for _, published := range pub.snapshot() {
		for _, msg := range flattenMessage(t, published.msg) {
			lines = append(lines, outputOf(msg).Text)
		}
	}
	return lines
}

func TestPointsSpendWithoutViewer(t *testing.T) {
	fx := newAutoFixture(manual("spend", act("points_spend", obj{"amount": 60})))
	rec := fx.run(t, "spend", "viewerless", nil)
	assert.Equal(t, []string{"0:points_spend:failed:no_viewer"}, statuses(rec))
}

func TestNamedTimerActionsThroughPipeline(t *testing.T) {
	for _, tc := range []struct {
		action string
		status string
		found  bool
	}{
		{action: "countdown_stop", status: "ok:clock2", found: true},
		{action: "countdown_stop", status: "failed:not_running"},
		{action: "cooldown_reset", status: "ok:clock2"},
	} {
		t.Run(fmt.Sprintf("%s/%s", tc.action, tc.status), func(t *testing.T) {
			auto := command("!change", obj{}, cond("perm", obj{"level": "mod"}))
			auto.Definition.Actions = []automation.Action{act(tc.action, obj{"name": "Clock2"})}
			countdowns := &fakeCountdown{result: CountdownResult{Found: tc.found}}
			long := &fakeLongCooldowns{}
			p, runs := (autoRig{automations: []automation.Automation{auto}, long: long}).pipeline()
			p.automations.Countdowns = countdowns
			process(t, p, chatEnv("!change", "moderator"))
			require.Len(t, runs.recs, 1)
			assert.Equal(t, []string{"0:" + tc.action + ":" + tc.status}, statuses(runs.recs[0]))
			assert.Equal(t, "clock2", countdowns.name+long.reset.Name)
		})
	}
}
