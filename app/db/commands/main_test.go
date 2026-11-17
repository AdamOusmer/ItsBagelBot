// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/enttest"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/health"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCommandShutdownStopsWithOrWithoutSignal(t *testing.T) {
	for _, signaled := range []bool{false, true} {
		t.Run(strconv.FormatBool(signaled), func(t *testing.T) {
			client := testdb.Open(t, "command_shutdown", func(d, dsn string) *ent.Client {
				return enttest.Open(t, d, dsn)
			})
			r := repository.NewCommands(client, bustest.NewPublisher(), nil, zap.NewNop())
			t.Cleanup(func() { require.NoError(t, r.Close(context.Background())) })
			started := make(chan time.Time, 1)
			if signaled {
				started <- time.Now()
			}
			closed := make(chan struct{})
			shutdown := commandShutdown{closeIntake: func() {}, repo: r, log: zap.NewNop(), started: started}
			go func() { shutdown.stop(); close(closed) }()
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("shutdown waited for a cancellation signal")
			}
		})
	}
}

func TestShutdownBudgetFitsTheManifestGracePeriod(t *testing.T) {
	manifest, err := os.ReadFile("../../../deploy/k8s/commands.yaml")
	require.NoError(t, err)
	match := regexp.MustCompile(`terminationGracePeriodSeconds:\s*(\d+)`).FindAllSubmatch(manifest, -1)
	require.Len(t, match, 1)
	seconds, err := strconv.Atoi(string(match[0][1]))
	require.NoError(t, err)
	assert.Equal(t, terminationGrace, time.Duration(seconds)*time.Second)
	assert.Contains(t, string(manifest), "path: /drain")
	assert.Equal(t, health.DrainDelay, preStopDrain)

	assert.LessOrEqual(t, preStopDrain+shutdownBudget+exitReserve, terminationGrace)
	assert.Positive(t, shutdownBudget)
}
