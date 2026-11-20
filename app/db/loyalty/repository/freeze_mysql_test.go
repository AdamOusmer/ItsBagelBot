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

	"ItsBagelBot/internal/domain/event/data"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreezeMySQLCopyIsolation(t *testing.T) {
	for _, isolation := range []string{"READ-COMMITTED", "REPEATABLE-READ"} {
		t.Run(isolation, func(t *testing.T) {
			raw := loyaltyMySQL(t, "loyalty_freeze_copy_")
			repo := mysqlReplica(t, raw)
			ctx := t.Context()
			award := balanceWriter{channel: 1}.award(0)
			require.NoError(t, repo.ApplyWatchAward(ctx, award))

			probe := freezeCopyProbe{repo: repo, ready: make(chan struct{}), release: make(chan struct{})}
			probe.start(t, isolation)
			award.WindowID = "after-copy"
			assertFreezeCopyLocking(t, repo, award, isolation)
		})
	}
}

type freezeCopyProbe struct {
	repo    *Loyalty
	ready   chan struct{}
	release chan struct{}
}

func (p freezeCopyProbe) start(t *testing.T, isolation string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- p.run(ctx, isolation)
	}()
	t.Cleanup(func() {
		defer cancel()
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

func assertFreezeCopyLocking(t *testing.T, repo *Loyalty, award data.WatchAwardDTO, isolation string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err := repo.ApplyWatchAward(ctx, award)
	if isolation == "REPEATABLE-READ" {
		require.ErrorIs(t, err, context.DeadlineExceeded)
		return
	}
	require.NoError(t, err)
}

func TestFreezeMySQLSerializesResetAndCleanup(t *testing.T) {
	raw := loyaltyMySQL(t, "loyalty_freeze_race_")
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
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
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
