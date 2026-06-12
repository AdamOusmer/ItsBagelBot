package monitor

import (
	"log/slog"
	"os"
	"time"

	"github.com/newrelic/go-agent/v3/integrations/nrslog"
	"github.com/newrelic/go-agent/v3/newrelic"
)

const shutdownTimeout = 10 * time.Second

func New(service string, log *slog.Logger) (*newrelic.Application, error) {
	if os.Getenv("NEW_RELIC_LICENSE_KEY") == "" {
		log.Info("new relic disabled: no license key configured")
		return nil, nil
	}

	return newrelic.NewApplication(
		newrelic.ConfigAppName("ItsBagelBot-"+service),
		newrelic.ConfigDistributedTracerEnabled(true),
		newrelic.ConfigAppLogForwardingEnabled(true),
		newrelic.ConfigFromEnvironment(),
		nrslog.ConfigLogger(log),
	)
}

func Shutdown(app *newrelic.Application) {
	if app == nil {
		return
	}
	app.Shutdown(shutdownTimeout)
}
