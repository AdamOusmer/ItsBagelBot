package bus

import (
	"github.com/ThreeDotsLabs/watermill"

	"go.uber.org/zap"
)

// zapAdapter bridges watermill's logging interface onto our zap logger.
type zapAdapter struct {
	log *zap.Logger
}

func newZapAdapter(log *zap.Logger) watermill.LoggerAdapter {
	return zapAdapter{log: log}
}

func (a zapAdapter) Error(msg string, err error, fields watermill.LogFields) {
	a.log.Error(msg, append(zapFields(fields), zap.Error(err))...)
}

func (a zapAdapter) Info(msg string, fields watermill.LogFields) {
	a.log.Info(msg, zapFields(fields)...)
}

func (a zapAdapter) Debug(msg string, fields watermill.LogFields) {
	a.log.Debug(msg, zapFields(fields)...)
}

func (a zapAdapter) Trace(msg string, fields watermill.LogFields) {
	a.log.Debug(msg, zapFields(fields)...)
}

func (a zapAdapter) With(fields watermill.LogFields) watermill.LoggerAdapter {
	return zapAdapter{log: a.log.With(zapFields(fields)...)}
}

func zapFields(fields watermill.LogFields) []zap.Field {

	out := make([]zap.Field, 0, len(fields)+1)
	for k, v := range fields {
		out = append(out, zap.Any(k, v))
	}

	return out
}
