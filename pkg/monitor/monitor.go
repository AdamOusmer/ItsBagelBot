package monitor

import (
	"context"
	"time"

	"ItsBagelBot/pkg/env"

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
		// Kubernetes stdout is already forwarded by Fluent Bit. Keep the APM
		// agent from sending a second copy of every zap line to New Relic;
		// logs in context comes from the linking attributes WrapLogger and
		// TxnLogger stamp on each JSON line instead.
		newrelic.ConfigAppLogForwardingEnabled(false),
		newrelic.ConfigAppLogMetricsEnabled(true),
		// Reads NEW_RELIC_* variables, so the license key stays out of the
		// code and any setting can be overridden per deployment.
		newrelic.ConfigFromEnvironment(),
	)
}

// linkingCore stamps New Relic entity linking attributes on every log entry so
// the Fluent Bit pipeline delivers lines already tied to this service's APM
// entity. Metadata is looked up per write because the entity GUID only exists
// once the agent finishes connecting, which is after boot logging starts.
type linkingCore struct {
	zapcore.Core
	app *newrelic.Application
}

func (c linkingCore) With(fields []zapcore.Field) zapcore.Core {
	return linkingCore{Core: c.Core.With(fields), app: c.app}
}

func (c linkingCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {

	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}

	return checked
}

func (c linkingCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	return c.Core.Write(entry, append(fields, linkingFields(c.app.GetLinkingMetadata())...))
}

func linkingFields(md newrelic.LinkingMetadata) []zapcore.Field {

	if md.EntityGUID == "" {
		return nil
	}

	return []zapcore.Field{
		zap.String("entity.guid", md.EntityGUID),
		zap.String("entity.name", md.EntityName),
		zap.String("hostname", md.Hostname),
	}
}

// WrapLogger gives the service logger entity-level logs in context: every line
// carries the service's New Relic entity attributes. Called in the shared boot
// path, so all services pick it up. A nil app (no license key) is a no-op,
// keeping local development unmonitored.
func WrapLogger(log *zap.Logger, app *newrelic.Application) *zap.Logger {

	if app == nil {
		return log
	}

	return log.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return linkingCore{Core: core, app: app}
	}))
}

// TxnLogger returns log with the surrounding transaction's trace and span IDs
// attached, so lines written inside a handler join the distributed trace in
// New Relic. Without a transaction in ctx it returns log unchanged, which
// keeps it safe to call unconditionally at the top of handlers.
func TxnLogger(ctx context.Context, log *zap.Logger) *zap.Logger {
	return TraceLogger(newrelic.FromContext(ctx), log)
}

// TraceLogger is TxnLogger for call sites that already hold the transaction.
func TraceLogger(txn *newrelic.Transaction, log *zap.Logger) *zap.Logger {

	if txn == nil {
		return log
	}

	md := txn.GetTraceMetadata()
	if md.TraceID == "" {
		return log
	}

	return log.With(zap.String("trace.id", md.TraceID), zap.String("span.id", md.SpanID))
}

// Shutdown flushes remaining telemetry; safe on a nil application.
func Shutdown(app *newrelic.Application) {

	if app == nil {
		return
	}

	app.Shutdown(shutdownTimeout)
}
