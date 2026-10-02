// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package logger_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"ItsBagelBot/pkg/logger"
)

func TestNewSetsTheLevelAndReplacesTheGlobalLogger(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		wantDebug bool
	}{
		{name: "enables debug in development", env: "development", wantDebug: true},
		{name: "suppresses debug in production", env: "production"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log := logger.New(tc.env)

			assert.Equal(t, tc.wantDebug, log.Core().Enabled(zap.DebugLevel))
			assert.True(t, log.Core().Enabled(zap.InfoLevel))
			assert.Equal(t, log.Core(), zap.L().Core())
		})
	}
}

func TestAtomLevelSwitchesTheProductionLoggerAtRuntime(t *testing.T) {
	log := logger.New("production")
	assert.False(t, log.Core().Enabled(zap.DebugLevel))

	logger.AtomLevel.SetLevel(zap.DebugLevel)

	assert.True(t, log.Core().Enabled(zap.DebugLevel))
}
