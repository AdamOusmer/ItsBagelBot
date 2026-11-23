// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	loyaltyent "ItsBagelBot/app/db/loyalty/ent"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/projection"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type sqliteDebitStore struct {
	LoyaltyStore
	repo    *loyaltyrepo.Loyalty
	channel uint64
	mu      sync.Mutex
	keys    map[string]int
}

func (s *sqliteDebitStore) BalanceDebit(ctx context.Context, debit PointDebit) (DebitOutcome, error) {
	s.mu.Lock()
	s.keys[debit.Key]++
	s.mu.Unlock()
	result, err := s.repo.BalanceDebit(ctx, loyaltyrepo.BalanceDebit{
		UserID: debit.BroadcasterID, ViewerID: debit.ViewerID, ViewerLogin: debit.ViewerLogin,
		Value: debit.Value, Key: debit.Key, AccountCreatedAt: debit.AccountCreatedAt,
	})
	outcome := DebitOutcome{Spent: result.Spent, Replayed: result.Replayed}
	if result.Balance != nil {
		outcome.Balance = *result.Balance
	}
	return outcome, err
}

func newSQLiteDebitStore(t *testing.T, balance int64) *sqliteDebitStore {
	t.Helper()
	driver := entsql.OpenDB("sqlite3", completionSQL(t, "sqlite3", "spend"))
	client := loyaltyent.NewClient(loyaltyent.Driver(driver))
	require.NoError(t, client.Schema.Create(t.Context()))
	repo := loyaltyrepo.NewLoyalty(client, driver, nil, zap.NewNop())
	require.NoError(t, repo.EnsureWatchSchema(t.Context()))
	require.NoError(t, repo.EnsureBalanceCreditSchema(t.Context()))
	channel := freshChannel()
	require.NoError(t, repo.RestoreUser(t.Context(), channel, 101))
	require.NoError(t, client.Balance.Create().SetUserID(channel).SetViewerID(999).SetViewerLogin("alice").SetPoints(balance).Exec(t.Context()))
	return &sqliteDebitStore{repo: repo, channel: channel, keys: map[string]int{}}
}

func TestProcessPointsSpendUsesAtomicBalanceAndMatchingBranch(t *testing.T) {
	for _, tc := range []struct {
		name       string
		balance    int64
		deliveries [][]string
		left       int64
		lines      []string
		keys       map[string]int
	}{
		{name: "enough", balance: 100, deliveries: [][]string{{"first"}}, left: 40, lines: []string{"spent 40"}},
		{name: "insufficient", balance: 50, deliveries: [][]string{{"first"}}, left: 50, lines: []string{"refused 50"}},
		{name: "parallel", balance: 100, deliveries: [][]string{{"first", "second"}}, left: 40, lines: []string{"spent 40", "refused 40"}},
		{name: "retry", balance: 160, deliveries: [][]string{{"retry"}, {"retry"}}, left: 100, lines: []string{"spent 100", "spent 100"},
			keys: map[string]int{"recipe:retry>id-!spend:auto:id-!spend:0": 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newSQLiteDebitStore(t, tc.balance)
			lines := processSpends(t, store, tc.deliveries)
			row, _, err := store.repo.BalanceGet(t.Context(), store.channel, 999)
			require.NoError(t, err)
			assert.Equal(t, tc.left, row.Points)
			assert.ElementsMatch(t, tc.lines, lines)
			checkDebitKeys(t, tc.keys, store.keys)
		})
	}
}

func checkDebitKeys(t *testing.T, want, got map[string]int) {
	t.Helper()
	if want != nil {
		assert.Equal(t, want, got)
	}
}

func processSpends(t *testing.T, store *sqliteDebitStore, deliveries [][]string) []string {
	t.Helper()
	auto := command("!spend", obj{})
	auto.Definition.Actions = []automation.Action{act("points_spend", automation.PointsSpendAction{
		Amount:    60,
		OnOK:      []automation.Action{act("chat", obj{"text": "spent {points}"})},
		OnFailure: []automation.Action{act("chat", obj{"text": "refused {points}"})},
	})}
	pub := &fakePublisher{}
	state := lifecycleTestState(lifecycle.Active, 1)
	state.LastEngagedAt = time.Now()
	runtime := bindLifecycleRuntime(t, projection.NewStore(newHotPathTestClient(t)), store.channel, state)
	p, _ := (autoRig{automations: []automation.Automation{auto}, loyalty: store, pub: pub}).pipeline()
	p.runtime = runtime
	for _, batch := range deliveries {
		deliverSpendBatch(t, p, store.channel, batch)
	}
	return spendLines(t, pub)
}

func deliverSpendBatch(t *testing.T, p *Pipeline, channel uint64, ids []string) {
	t.Helper()
	var runs sync.WaitGroup
	for _, id := range ids {
		env := chatEnv("!spend", "")
		env.BroadcasterUserID, env.MsgID = strconv.FormatUint(channel, 10), id
		runs.Go(func() { process(t, p, env) })
	}
	runs.Wait()
}

func spendLines(t *testing.T, pub *fakePublisher) []string {
	t.Helper()
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
