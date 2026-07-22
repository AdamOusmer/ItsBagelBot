package monitor

import (
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/newrelic/go-agent/v3/integrations/logcontext-v2/nrzap"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
		// Logs in context: the agent forwards each zap line straight to New
		// Relic (there is no stdout log shipper in the fleet), and decoration
		// stamps the NR-LINKING metadata that ties every line to this entity
		// and, inside a transaction, to its trace.
		newrelic.ConfigAppLogForwardingEnabled(true),
		newrelic.ConfigAppLogDecoratingEnabled(true),
		newrelic.ConfigAppLogMetricsEnabled(true),
		// Reads NEW_RELIC_* variables, so the license key stays out of the
		// code and any setting can be overridden per deployment.
		newrelic.ConfigFromEnvironment(),
	)
}

// WrapLogger routes zap output through nrzap so every line is forwarded to the
// service's own New Relic entity (application-level logs in context). Called in
// the shared boot path, so all services pick it up. Per-transaction trace
// linking would additionally wrap with nrzap.WrapTransactionCore at each txn
// boundary; that stays out of the shared path. A nil app (no license key) is a
// no-op, keeping local development unmonitored.
func WrapLogger(log *zap.Logger, app *newrelic.Application) *zap.Logger {

	if app == nil {
		return log
	}

	return log.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		wrapped, _ := nrzap.WrapBackgroundCore(core, app)
		return wrapped
	}))
}

// Shutdown flushes remaining telemetry; safe on a nil application.
func Shutdown(app *newrelic.Application) {

	if app == nil {
		return
	}

	app.Shutdown(shutdownTimeout)
}
