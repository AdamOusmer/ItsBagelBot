// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"encoding/json"
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const followFlow = `{"version":1,"trigger":{"type":"event","params":{"kind":"channel.follow"}},"actions":[{"type":"chat","params":{"text":"hi"}},{"type":"points_add","params":{"amount":5}}]}`

type alertRig struct {
	client  *ent.Client
	pub     *bustest.Publisher
	repo    *repository.Automations
	modules *repository.Modules
}

func newAlertRig(t *testing.T, name testdb.Name) alertRig {
	t.Helper()
	client := testdb.Open(t, name, func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	pub := bustest.NewPublisher()
	repo := repository.NewAutomations(client, pub, zap.NewNop())
	modules := repository.NewModules(client, pub, nil, zap.NewNop())
	t.Cleanup(func() { modules.Close(context.Background()) })
	repo.BindModules(modules)
	return alertRig{client: client, pub: pub, repo: repo, modules: modules}
}

func (r alertRig) seedAlerts(t *testing.T, configs string) int {
	t.Helper()
	views, err := r.modules.SetNow(t.Context(), 1001, "alerts", true, codec.RawMessage(configs))
	require.NoError(t, err)
	require.Len(t, views, 1)
	return views[0].Revision
}

func (r alertRig) alertConfig(t *testing.T) map[string]any {
	t.Helper()
	views, err := r.modules.List(t.Context(), 1001)
	require.NoError(t, err)
	require.Len(t, views, 1)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(views[0].Configs, &cfg))
	return cfg
}

func TestUpgradeAlertTurnsAlertOffAndInsertsTogether(t *testing.T) {
	r := newAlertRig(t, "modalertupgrade")
	rev := r.seedAlerts(t, `{"followEnabled":"on","followMessage":"hello","subMessage":"keep"}`)

	saved, err := r.repo.UpgradeAlert(t.Context(), 1001,
		repository.AutomationInput{Name: "Follow alert", Enabled: true, Definition: []byte(followFlow)},
		repository.AlertUpgrade{EnableKey: "followEnabled", ExpectedModuleRev: &rev})

	require.NoError(t, err)
	assert.Equal(t, int64(1), saved.Revision)
	cfg := r.alertConfig(t)
	assert.Equal(t, "off", cfg["followEnabled"])
	assert.Equal(t, "hello", cfg["followMessage"])
	assert.Equal(t, "keep", cfg["subMessage"])
	assert.Len(t, r.pub.On(data.SubjectAutomationChanged), 1)
	assert.Len(t, r.pub.On(data.SubjectModuleChanged), 2)
}

func TestUpgradeAlertRollsBackBothSides(t *testing.T) {
	stale := 99
	cases := []struct {
		name    testdb.Name
		before  func(t *testing.T, r alertRig)
		input   string
		rev     *int
		wantErr func(t *testing.T, err error)
	}{
		{
			name:  "modalertstale",
			input: "Follow alert",
			rev:   &stale,
			wantErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, repository.ErrRevisionConflict)
			},
		},
		{
			name: "modalertdupname",
			before: func(t *testing.T, r alertRig) {
				_, err := r.repo.Upsert(t.Context(), 1001, repository.AutomationInput{Name: "Follow alert", Definition: []byte(followFlow)})
				require.NoError(t, err)
			},
			input: "follow ALERT",
			wantErr: func(t *testing.T, err error) {
				assert.Equal(t, rpc.CodeConflict, refusalOf(t, err).Code)
			},
		},
	}
	for _, tc := range cases {
		t.Run(string(tc.name), func(t *testing.T) {
			r := newAlertRig(t, tc.name)
			r.seedAlerts(t, `{"followEnabled":"on"}`)
			if tc.before != nil {
				tc.before(t, r)
			}
			before, err := r.repo.List(t.Context(), 1001)
			require.NoError(t, err)

			_, err = r.repo.UpgradeAlert(t.Context(), 1001,
				repository.AutomationInput{Name: tc.input, Enabled: true, Definition: []byte(followFlow)},
				repository.AlertUpgrade{EnableKey: "followEnabled", ExpectedModuleRev: tc.rev})

			tc.wantErr(t, err)
			assert.Equal(t, "on", r.alertConfig(t)["followEnabled"])
			after, err := r.repo.List(t.Context(), 1001)
			require.NoError(t, err)
			assert.Len(t, after, len(before))
		})
	}
}

func TestUpgradeAlertRefusesUnknownSwitch(t *testing.T) {
	r := newAlertRig(t, "modalertbadkey")
	_, err := r.repo.UpgradeAlert(t.Context(), 1001,
		repository.AutomationInput{Name: "Follow alert", Definition: []byte(followFlow)},
		repository.AlertUpgrade{EnableKey: "adsEnabled"})
	assert.Equal(t, rpc.CodeInvalid, refusalOf(t, err).Code)
}

func TestUpgradeAlertWithoutModuleRowOnlyInserts(t *testing.T) {
	r := newAlertRig(t, "modalertnorow")
	saved, err := r.repo.UpgradeAlert(t.Context(), 1001,
		repository.AutomationInput{Name: "Follow alert", Definition: []byte(followFlow)},
		repository.AlertUpgrade{EnableKey: "followEnabled"})
	require.NoError(t, err)
	assert.NotEmpty(t, saved.ID)
	assert.Empty(t, r.pub.On(data.SubjectModuleChanged))
}
