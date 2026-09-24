// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

func New(service string, log *zap.Logger) (*newrelic.Application, error) {

	if env.Get("NEW_RELIC_LICENSE_KEY", "") == "" {
		log.Info("new relic disabled: no license key configured")
		return nil, nil
	}

	app, err := newrelic.NewApplication(
		newrelic.ConfigAppName("ItsBagelBot-"+service),
		newrelic.ConfigDistributedTracerEnabled(true),
		newrelic.ConfigAppLogForwardingEnabled(false),
		newrelic.ConfigAppLogMetricsEnabled(true),
		newrelic.ConfigFromEnvironment(),
	)
	if err != nil {
		return nil, err
	}

	return app, nil
}

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

func WrapLogger(log *zap.Logger, app *newrelic.Application) *zap.Logger {

	if app == nil {
		return log
	}

	return log.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return linkingCore{Core: core, app: app}
	}))
}

func TxnLogger(ctx context.Context, log *zap.Logger) *zap.Logger {
	return TraceLogger(newrelic.FromContext(ctx), log)
}

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

func Shutdown(app *newrelic.Application) {

	if app == nil {
		return
	}

	app.Shutdown(shutdownTimeout)
}
