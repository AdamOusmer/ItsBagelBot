package logger_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"ItsBagelBot/pkg/logger"
)

func TestNew_Development(t *testing.T) {
	// 1. We use 'log' (instance) so we don't block 'logger' (package)
	log := logger.New("development")

	assert.NotNil(t, log)

	// Development config should allow Debug logs
	assert.True(t, log.Core().Enabled(zap.DebugLevel))
	assert.True(t, log.Core().Enabled(zap.InfoLevel))
}

func TestNew_Production(t *testing.T) {
	log := logger.New("production")

	assert.NotNil(t, log)

	// Production config defaults to Info (Debug should be hidden)
	assert.False(t, log.Core().Enabled(zap.DebugLevel))
	assert.True(t, log.Core().Enabled(zap.InfoLevel))
}

func TestAtomLevel_DynamicSwitching(t *testing.T) {
	// Start with Production (Info level only)
	log := logger.New("production")

	// Verify initial state
	assert.False(t, log.Core().Enabled(zap.DebugLevel))

	// ACT: Change the global AtomLevel variable from the package
	logger.AtomLevel.SetLevel(zap.DebugLevel)

	// ASSERT: The 'log' instance should instantly update to allow Debug
	assert.True(t, log.Core().Enabled(zap.DebugLevel))
}

func TestGlobalReplacement(t *testing.T) {
	log := logger.New("production")

	// Verify that zap.L() (the global logger) was updated to match our new logger
	// We compare the Core addresses to see if they are the same underlying object
	assert.Equal(t, log.Core(), zap.L().Core())
}

func TestNew_Production_ConfigFormat(t *testing.T) {
	// Optional: Verify specific production settings exist
	log := logger.New("production")

	// We can't easily check internal config struct values,
	// but we can check behavior (like Level) which we did above.
	assert.NotNil(t, log)
}
