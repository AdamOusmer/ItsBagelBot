package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var AtomLevel = zap.NewAtomicLevel()

func New(env string) *zap.Logger {

	var config zap.Config

	if env == "production" {
		config = zap.NewProductionConfig()

		config.EncoderConfig.TimeKey = "timestamp"
		config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

		AtomLevel.SetLevel(zap.InfoLevel)
		config.Level = AtomLevel

		config.EncoderConfig.LevelKey = "level"
		config.EncoderConfig.MessageKey = "message"

		config.Sampling = &zap.SamplingConfig{ // enable log sampling to reduce log volume
			Initial:    100,
			Thereafter: 100,
		}
	} else {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	logger, err := config.Build()
	if err != nil {
		panic(err)
	}

	zap.ReplaceGlobals(logger)
	return logger
}
