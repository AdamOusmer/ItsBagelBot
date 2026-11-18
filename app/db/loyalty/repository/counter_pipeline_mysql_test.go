// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"net"
	"sync/atomic"
	"testing"

	"ItsBagelBot/internal/domain/event/data"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

type mysqlQueryCounter struct {
	net.Conn
	queries *atomic.Int64
}

func (c mysqlQueryCounter) Write(packet []byte) (int, error) {
	if isMySQLQueryPacket(packet) {
		c.queries.Add(1)
	}
	return c.Conn.Write(packet)
}

func isMySQLQueryPacket(packet []byte) bool {
	const comQuery byte = 0x03
	return len(packet) > 4 && packet[3] == 0 && packet[4] == comQuery
}

func countMySQLQueries(t *testing.T, queries *atomic.Int64) func(*mysql.Config) {
	return func(cfg *mysql.Config) {
		cfg.TLS, cfg.TLSConfig = nil, "false"
		require.NoError(t, cfg.Apply(mysql.EnableCompression(false)))
		dialer := net.Dialer{Timeout: cfg.Timeout}
		cfg.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			return mysqlQueryCounter{Conn: conn, queries: queries}, nil
		}
	}
}

func TestCounterMySQLFreshBatchesUseTwoRoundTrips(t *testing.T) {
	var queries atomic.Int64
	raw := loyaltyMySQL(t, "loyalty_counter_pipeline_", countMySQLQueries(t, &queries))
	raw.SetMaxOpenConns(1)
	repo := mysqlReplica(t, raw)
	p := uncachedCounterProcessor(t, repo)
	events := counterLaneEvents()
	channel := &events[1]
	channel.Bumps = append(channel.Bumps, replicaEntryBump(7, 2))
	require.NoError(t, repo.RestoreUser(t.Context(), channel.UserID, 1))

	for _, lane := range []struct {
		name  string
		event data.CounterBumpedDTO
	}{{"bot", events[0]}, {"channel", *channel}} {
		t.Run(lane.name, func(t *testing.T) {
			before := queries.Load()
			require.NoError(t, p.Process(t.Context(), lane.event))
			require.EqualValues(t, 2, queries.Load()-before)
		})
	}
	assertCounterLaneTotals(t, repo)
	ref := CounterRef{UserID: channel.UserID, Name: "hugs"}
	_, value, found, err := repo.CounterGet(t.Context(), ref, SetTarget{ViewerID: 7})
	require.NoError(t, err)
	require.Equal(t, [2]any{int64(2), true}, [2]any{value, found})
}

func TestCounterMySQLPipelineVerifiesTrialsAndSkipsReplaysAndRetiredOwners(t *testing.T) {
	raw := loyaltyMySQL(t, "loyalty_counter_paths_")
	raw.SetMaxOpenConns(1)
	repo := mysqlReplica(t, raw)
	p := uncachedCounterProcessor(t, repo)
	trial := trialBatch("unpromoted", data.CounterBumpEntry{Name: data.CounterTrialDecoded, Delta: 4})
	trial.Bumps = append(trial.Bumps, replicaEntryBump(7, 2))
	require.NoError(t, repo.RestoreUser(t.Context(), trial.UserID, 1))
	verifyMySQLTrialRetry(t, raw, p, trial)

	promoted, err := repo.PromoteTrial(t.Context(), trial.UserID)
	require.NoError(t, err)
	require.True(t, promoted)
	next := trialBatch("promoted", data.CounterBumpEntry{Name: data.CounterTrialDecoded, Delta: 3})
	deliveries := [2]error{p.Process(t.Context(), trial), p.Process(t.Context(), next)}
	require.Equal(t, [2]error{}, deliveries)
	ref := CounterRef{UserID: trial.UserID, Name: data.CounterMessagesProcessed}
	_, value, found, err := repo.CounterGet(t.Context(), ref, SetTarget{})
	require.NoError(t, err)
	require.Equal(t, [2]any{int64(7), true}, [2]any{value, found})

	require.NoError(t, repo.DeleteAccount(t.Context(), trial.UserID, 1))
	require.NoError(t, p.Process(t.Context(), trialBatch("retired", data.CounterBumpEntry{Name: data.CounterTrialDecoded, Delta: 9})))
	_, _, found, err = repo.CounterGet(t.Context(), ref, SetTarget{})
	require.NoError(t, err)
	require.False(t, found)
	assertCounterReceipts(t, raw, 3)
}

func verifyMySQLTrialRetry(t *testing.T, raw *sql.DB, p *CounterProcessor, trial data.CounterBumpedDTO) {
	t.Helper()
	_, err := raw.ExecContext(t.Context(), `CREATE TRIGGER counter_trial_race AFTER INSERT ON counter_entries FOR EACH ROW
		BEGIN
			IF COALESCE(@counter_trial_race, 0)=0 THEN
				SET @counter_trial_race=1;
				INSERT INTO counters (user_id,name,scope,value,created_at,updated_at)
				VALUES (NEW.user_id,'trial_promoted','channel',1,NOW(),NOW());
			END IF;
		END`)
	require.NoError(t, err)
	before := counterSessionStatements(t, raw)
	require.NoError(t, p.Process(t.Context(), trial))
	after := counterSessionStatements(t, raw)
	require.Equal(t, [2]int{6, 1}, [2]int{after["Com_select"] - before["Com_select"], after["Com_rollback"] - before["Com_rollback"]})
}
