// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package logger_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"ItsBagelBot/pkg/logger"
)

func TestNew_Development(t *testing.T) {
	log := logger.New("development")

	assert.NotNil(t, log)

	assert.True(t, log.Core().Enabled(zap.DebugLevel))
	assert.True(t, log.Core().Enabled(zap.InfoLevel))
}

func TestNew_Production(t *testing.T) {
	log := logger.New("production")

	assert.NotNil(t, log)

	assert.False(t, log.Core().Enabled(zap.DebugLevel))
	assert.True(t, log.Core().Enabled(zap.InfoLevel))
}

func TestAtomLevel_DynamicSwitching(t *testing.T) {
	log := logger.New("production")

	assert.False(t, log.Core().Enabled(zap.DebugLevel))

	logger.AtomLevel.SetLevel(zap.DebugLevel)

	assert.True(t, log.Core().Enabled(zap.DebugLevel))
}

func TestGlobalReplacement(t *testing.T) {
	log := logger.New("production")

	assert.Equal(t, log.Core(), zap.L().Core())
}

func TestNew_Production_ConfigFormat(t *testing.T) {
	log := logger.New("production")

	assert.NotNil(t, log)
}
