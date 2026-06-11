package monitor

import (
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/newrelic/go-agent/v3/integrations/logcontext-v2/nrzap"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const shutdownTimeout = 10 * time.Second

// New starts the New Relic application for one service. Without a license key
// it returns nil, which the agent treats as a no-op everywhere (nil receivers
// are safe on Application and Transaction), so services run unmonitored in
// local development without a single branch in the calling code.
func New(service string, log *zap.Logger) (*newrelic.Application, error) {

	if env.Get("NEW_RELIC_LICENSE_KEY", "") == "" {
		log.Info("new relic disabled: no license key configured")
		return nil, nil
	}

	return newrelic.NewApplication(
		newrelic.ConfigAppName("ItsBagelBot-"+service),
		newrelic.ConfigDistributedTracerEnabled(true),
		newrelic.ConfigAppLogForwardingEnabled(true),
		// Reads NEW_RELIC_* variables, so the license key stays out of the
		// code and any setting can be overridden per deployment.
		newrelic.ConfigFromEnvironment(),
	)
}

// WrapLogger forwards every log line to New Relic alongside stdout. With a
// nil application the original logger is returned untouched.
func WrapLogger(log *zap.Logger, app *newrelic.Application) *zap.Logger {

	if app == nil {
		return log
	}

	core, err := nrzap.WrapBackgroundCore(log.Core(), app)
	if err != nil {
		log.Warn("failed to wrap logger for new relic, keeping plain logger", zap.Error(err))
		return log
	}

	return zap.New(core)
}

// Shutdown flushes remaining telemetry; safe on a nil application.
func Shutdown(app *newrelic.Application) {

	if app == nil {
		return
	}

	app.Shutdown(shutdownTimeout)
}
