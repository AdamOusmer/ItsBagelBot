package monitor

import (
	"time"

	"ItsBagelBot/pkg/env"

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
		// Kubernetes stdout is already forwarded by Fluent Bit. Keep the APM
		// agent from sending a second copy of every zap line to New Relic.
		newrelic.ConfigAppLogForwardingEnabled(false),
		newrelic.ConfigAppLogMetricsEnabled(true),
		// Reads NEW_RELIC_* variables, so the license key stays out of the
		// code and any setting can be overridden per deployment.
		newrelic.ConfigFromEnvironment(),
	)
}

// WrapLogger is kept at service call sites so monitor setup stays centralized.
// Logs are intentionally not wrapped with nrzap: the cluster Fluent Bit pipeline
// owns log shipping, and nrzap would forward duplicate log events without
// decorating the stdout line that Fluent Bit sees.
func WrapLogger(log *zap.Logger, app *newrelic.Application) *zap.Logger {

	_ = app
	return log
}

// Shutdown flushes remaining telemetry; safe on a nil application.
func Shutdown(app *newrelic.Application) {

	if app == nil {
		return
	}

	app.Shutdown(shutdownTimeout)
}
