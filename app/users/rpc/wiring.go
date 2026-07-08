package rpc

import (
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/repository"
)

// Wiring bundles the process-wide handles every Subscribe* verb group needs:
// the NATS connection, the users repository, the New Relic app, the shared
// queue group, and the logger. Per-surface subjects and prefixes stay explicit
// arguments so each subscriber still declares the subjects it owns.
//
// adminauth is the exception: it holds the ent client directly (not the repo),
// so it reads NC/App/Queue/Log from here and takes its *ent.Client separately.
type Wiring struct {
	NC    *nats.Conn
	Repo  *repository.Users
	App   *newrelic.Application
	Queue string
	Log   *zap.Logger
}
