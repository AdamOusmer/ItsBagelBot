// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/db"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type variableRaceDriver struct {
	dialect.Driver
	reads     atomic.Int32
	deadlocks atomic.Int32
	ready     sync.WaitGroup
}

func (d *variableRaceDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	tx, err := d.Driver.Tx(ctx)
	return &variableRaceTx{Tx: tx, race: d}, err
}

func (d *variableRaceDriver) observe(err error) {
	var conflict *mysql.MySQLError
	if errors.As(err, &conflict) && conflict.Number == db.MySQLDeadlock {
		d.deadlocks.Add(1)
	}
}

type variableRaceTx struct {
	dialect.Tx
	race *variableRaceDriver
}

func (tx *variableRaceTx) Query(ctx context.Context, query string, args, result any) error {
	err := tx.Tx.Query(ctx, query, args, result)
	tx.race.observe(err)
	if strings.HasPrefix(query, "SELECT") && tx.race.reads.Add(1) <= 2 {
		tx.race.ready.Done()
		tx.race.ready.Wait()
	}
	return err
}

func (tx *variableRaceTx) Exec(ctx context.Context, query string, args, result any) error {
	err := tx.Tx.Exec(ctx, query, args, result)
	tx.race.observe(err)
	return err
}

func TestVariableMySQLMissingRowDeadlockRegression(t *testing.T) {
	seed, raw := mysqlStateRepo(t)
	require.NoError(t, seed.VariableSet(t.Context(), viewerVars, "seed", "0"))
	drv := entsql.OpenDB("mysql", raw)
	race := &variableRaceDriver{Driver: drv}
	race.ready.Add(2)
	client := ent.NewClient(ent.Driver(race))
	repo := repository.NewRecipeState(client, drv)
	errs := concurrently(2, func(ctx context.Context, i int) error {
		owner := repository.VariableOwner{UserID: viewerVars.UserID, Scope: automation.VariableScopeViewer, ViewerID: uint64(100 + i)}
		return repo.VariableAdd(ctx, owner, "score", 1)
	})
	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Zero(t, race.deadlocks.Load(), "missing-row reads must not take gap locks before the names guard")
	var rows, total int
	require.NoError(t, raw.QueryRow("SELECT COUNT(*), SUM(value) FROM variables WHERE name = 'score'").Scan(&rows, &total))
	assert.Equal(t, [2]int{2, 2}, [2]int{rows, total})
}

func TestVariableMySQLWritesAvoidUnrelatedLocks(t *testing.T) {
	owners := []repository.VariableOwner{viewerVars, {UserID: viewerVars.UserID, ViewerID: 8, Scope: automation.VariableScopeViewer}}
	for _, tc := range []struct {
		name       string
		lockViewer uint64
		lockName   string
		writeName  string
		writers    []repository.VariableOwner
	}{
		{"existing writes bypass names guard", 0, "#names", "score", owners},
		{"new names bypass existing viewer locks", viewerVars.ViewerID, "score", "fresh", owners[1:]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, raw := mysqlStateRepo(t)
			for _, owner := range owners {
				require.NoError(t, repo.VariableSet(t.Context(), owner, "score", "0"))
			}
			tx, err := raw.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = tx.Rollback() })
			_, err = tx.ExecContext(t.Context(), "UPDATE variables SET revision = revision + 1 WHERE user_id = ? AND scope = ? AND viewer_id = ? AND name = ?", viewerVars.UserID, viewerVars.Scope, tc.lockViewer, tc.lockName)
			require.NoError(t, err)
			for _, owner := range tc.writers {
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				err := repo.VariableAdd(ctx, owner, tc.writeName, 1)
				cancel()
				require.NoError(t, err)
			}
		})
	}
}
