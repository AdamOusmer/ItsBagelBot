// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreezeMySQLCopyIsolation(t *testing.T) {
	for _, tc := range []struct {
		isolation string
		locks     balanceRecordLocks
	}{
		{"READ-COMMITTED", balanceRecordLocks{}},
		{"REPEATABLE-READ", balanceRecordLocks{any: true, shared: true}},
	} {
		t.Run(tc.isolation, func(t *testing.T) {
			raw := freezeMySQL(t, "loyalty_freeze_copy_")
			repo := mysqlReplica(t, raw)
			ctx := t.Context()
			award := balanceWriter{channel: 1}.award(0)
			require.NoError(t, repo.ApplyWatchAward(ctx, award))

			probe := freezeCopyProbe{repo: repo, ready: make(chan struct{}), release: make(chan struct{})}
			probe.start(t, tc.isolation)
			require.Equal(t, tc.locks, readBalanceRecordLocks(t, raw))
			if tc.locks.any {
				return
			}
			award.WindowID = "after-copy"
			started := time.Now()
			require.NoError(t, repo.ApplyWatchAward(ctx, award))
			require.LessOrEqual(t, time.Since(started), time.Second, "watch award during copy")
		})
	}
}

func freezeMySQL(t *testing.T, prefix string) *sql.DB {
	t.Helper()
	return loyaltyMySQL(t, prefix, func(cfg *mysql.Config) {
		cfg.Params["innodb_lock_wait_timeout"] = "5"
	})
}

type balanceRecordLocks struct{ any, shared bool }

func readBalanceRecordLocks(t *testing.T, raw *sql.DB) balanceRecordLocks {
	t.Helper()
	var locks balanceRecordLocks
	err := raw.QueryRowContext(t.Context(), `SELECT COUNT(*) > 0, COUNT(CASE
		WHEN LOCK_STATUS = 'GRANTED' AND LOCK_MODE IN ('S', 'S,REC_NOT_GAP') AND LOCK_DATA <> 'supremum pseudo-record' THEN 1 END) > 0
		FROM performance_schema.data_locks
		WHERE OBJECT_SCHEMA = DATABASE() AND OBJECT_NAME = 'balances' AND LOCK_TYPE = 'RECORD'`).Scan(&locks.any, &locks.shared)
	require.NoError(t, err)
	return locks
}

type freezeCopyProbe struct {
	repo    *Loyalty
	ready   chan struct{}
	release chan struct{}
}

func (p freezeCopyProbe) start(t *testing.T, isolation string) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- p.run(context.Background(), isolation)
	}()
	t.Cleanup(func() {
		close(p.release)
		assert.NoError(t, <-done)
	})
	select {
	case <-p.ready:
	case err := <-done:
		t.Fatalf("copy transaction stopped before probe: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("copy transaction did not become ready")
	}
}

func (p freezeCopyProbe) run(ctx context.Context, isolation string) error {
	if isolation == "READ-COMMITTED" {
		_, err := p.repo.withFreezeRow(ctx, 1, func(ctx context.Context, tx *sql.Tx, _ FreezeState) (freezeChange, error) {
			return freezeChange{}, p.holdCopy(ctx, tx, "READ-COMMITTED")
		})
		return err
	}
	conn, err := p.repo.sqldb.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return p.holdCopy(ctx, tx, "REPEATABLE-READ")
}

func (p freezeCopyProbe) holdCopy(ctx context.Context, tx *sql.Tx, isolation string) error {
	var reported string
	if err := tx.QueryRowContext(ctx, "SELECT @@transaction_isolation").Scan(&reported); err != nil {
		return err
	}
	if reported != isolation {
		return fmt.Errorf("copy isolation: got %s, want %s", reported, isolation)
	}
	if _, err := tx.ExecContext(ctx, freezeCopy, "probe", 1); err != nil {
		return err
	}
	close(p.ready)
	<-p.release
	return nil
}

func TestFreezeMySQLSerializesResetAndCleanup(t *testing.T) {
	raw := freezeMySQL(t, "loyalty_freeze_race_")
	repo := mysqlReplica(t, raw)
	before := readInnodbLocks(t, raw)
	for round := range 20 {
		channel := uint64(round + 1)
		runFreezeMySQLRace(t, repo, channel)
	}
	after := readInnodbLocks(t, raw)
	assert.Equal(t, before.deadlocks, after.deadlocks)
}

func runFreezeMySQLRace(t *testing.T, repo *Loyalty, channel uint64) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, repo.ApplyWatchAward(ctx, balanceWriter{channel: channel}.award(0)))
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, op := range []struct {
		name string
		run  func() error
	}{
		{"freeze", func() error {
			_, _, err := repo.Freeze(ctx, FreezeRequest{UserID: channel, Holder: "mod", Minutes: 5})
			return err
		}},
		{"reset", func() error {
			_, err := repo.BalanceReset(ctx, channel)
			if errors.Is(err, ErrFrozen) {
				return nil
			}
			return err
		}},
		{"delete", func() error { return repo.DeleteAccount(ctx, channel, balanceCreated) }},
	} {
		workers.Go(func() {
			<-start
			assert.NoError(t, op.run(), op.name)
		})
	}
	close(start)
	workers.Wait()
}
