// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/testdb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDowngradeToAlertSetsLineAndDeletesRowTogether(t *testing.T) {
	r := newAlertRig(t, "modalertdown")
	rev := r.seedAlerts(t, `{"followEnabled":"off","subMessage":"keep"}`)
	row, err := r.repo.Upsert(t.Context(), 1001, repository.AutomationInput{Name: "Flow", Enabled: true, Definition: []byte(followFlow)})
	require.NoError(t, err)

	err = r.repo.DowngradeToAlert(t.Context(), 1001, row.ID, repository.AlertDowngrade{
		EnableKey: "followEnabled", Message: `hi "there"`, Enabled: true,
		ExpectedRevision: &row.Revision, ExpectedModuleRev: &rev,
	})

	require.NoError(t, err)
	cfg := r.alertConfig(t)
	assert.Equal(t, "on", cfg["followEnabled"])
	assert.Equal(t, `hi "there"`, cfg["followMessage"])
	assert.Equal(t, "keep", cfg["subMessage"])
	rows, err := r.repo.List(t.Context(), 1001)
	require.NoError(t, err)
	assert.Empty(t, rows)
	assert.Len(t, r.pub.On(data.SubjectAutomationChanged), 2)
}

func TestDowngradeToAlertKeepsAnOffFlowOff(t *testing.T) {
	r := newAlertRig(t, "modalertdownoff")
	row, err := r.repo.Upsert(t.Context(), 1001, repository.AutomationInput{Name: "Flow", Definition: []byte(followFlow)})
	require.NoError(t, err)

	require.NoError(t, r.repo.DowngradeToAlert(t.Context(), 1001, row.ID, repository.AlertDowngrade{EnableKey: "raidEnabled", Message: "x"}))

	views, err := r.modules.List(t.Context(), 1001)
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.False(t, views[0].IsEnabled)
	assert.Equal(t, "off", r.alertConfig(t)["raidEnabled"])
}

func TestDowngradeToAlertRollsBackBothSides(t *testing.T) {
	stale := 99
	staleRow := int64(99)
	cases := []struct {
		name  string
		alert repository.AlertDowngrade
	}{
		{"modalertdownrev", repository.AlertDowngrade{EnableKey: "followEnabled", Message: "new", Enabled: true, ExpectedModuleRev: &stale}},
		{"modalertdownrow", repository.AlertDowngrade{EnableKey: "followEnabled", Message: "new", Enabled: true, ExpectedRevision: &staleRow}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newAlertRig(t, testdb.Name(tc.name))
			r.seedAlerts(t, `{"followMessage":"old"}`)
			row, err := r.repo.Upsert(t.Context(), 1001, repository.AutomationInput{Name: "Flow", Definition: []byte(followFlow)})
			require.NoError(t, err)

			err = r.repo.DowngradeToAlert(t.Context(), 1001, row.ID, tc.alert)

			require.ErrorIs(t, err, repository.ErrRevisionConflict)
			assert.Equal(t, "old", r.alertConfig(t)["followMessage"])
			rows, err := r.repo.List(t.Context(), 1001)
			require.NoError(t, err)
			assert.Len(t, rows, 1)
		})
	}
}

func TestDowngradeToAlertRefusals(t *testing.T) {
	r := newAlertRig(t, "modalertdownrefuse")
	row, err := r.repo.Upsert(t.Context(), 1001, repository.AutomationInput{Name: "Flow", Definition: []byte(followFlow)})
	require.NoError(t, err)

	err = r.repo.DowngradeToAlert(t.Context(), 1001, row.ID, repository.AlertDowngrade{EnableKey: "adsEnabled"})
	assert.Equal(t, rpc.CodeInvalid, refusalOf(t, err).Code)

	err = r.repo.DowngradeToAlert(t.Context(), 2002, row.ID, repository.AlertDowngrade{EnableKey: "followEnabled"})
	require.ErrorIs(t, err, repository.ErrAutomationNotFound)
}
