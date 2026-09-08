// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql/driver"
	"net"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/newrelic/go-agent/v3/newrelic/sqlparse"
	"go.uber.org/zap"
)

// connectSegment names the New Relic segment that covers establishing one
// MySQL connection: TCP, TLS, mTLS client auth, MySQL auth and the session-var
// round trip.
//
// Measurement (production, 2026-09-07): a cold connect to the OCI HeatWave NLB
// took ~205ms over 6 round trips, and none of it appeared anywhere in APM.
// This package used to open the pool with sql.Open("nrmysql", dsn); nrmysql
// instruments query and exec only, and the wrapConnector it installs passes
// Connect straight through without a segment, so every handshake landed in a
// transaction as unattributed time. That is the same failure mode as the gate
// wait (see gateWaitSegment in gate.go): a real, large, recurring cost with no
// name on it.
//
// Alternatives considered. (1) Keep sql.Open and accept the blind spot:
// rejected, connect time is the dominant DB latency on this fleet (~90% of
// wall time is transport, see connMaxIdleTime in provider.go) and it is the
// one part that was never measurable in the trace. (2) Wrap the driver rather
// than the connector: rejected, database/sql only reaches a driver's
// connect path through the Connector when one is supplied, and going through
// the driver name means re-parsing the DSN on every open. Building the
// connector from the *mysql.Config directly also keeps the password out of a
// DSN string entirely.
//
// The Info log exists because most connects have no transaction on their
// context: database/sql opens connections from its own background opener
// goroutine, so a segment alone would leave exactly the cold-start connects
// that hurt most invisible. It carries the address and the elapsed time, never
// credentials.
const connectSegment = "db.connect"

// timedConnector wraps the real MySQL connector so one connect is timed,
// logged and, when the caller's context carries a transaction, reported as a
// segment. It embeds driver.Connector so the optional interfaces database/sql
// probes for on the wrapped value keep working.
type timedConnector struct {
	driver.Connector

	// addr is mysql.Config.Addr, host:port or a socket path. Kept as its own
	// field rather than read back off the config because mysql.NewConnector
	// clones and normalizes what it is given.
	addr string
}

func (c timedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	segment := startConnectSegment(ctx)
	started := time.Now()

	conn, err := c.Connector.Connect(ctx)

	endConnectSegment(segment)
	c.logConnect(time.Since(started), err)
	return conn, err
}

// logConnect reports every open. Failures log at warn rather than error: a
// single refused connect is retried by database/sql and is not by itself an
// outage, and HealthCheck (pkg/db/health.go) is what decides readiness.
func (c timedConnector) logConnect(elapsed time.Duration, err error) {
	if err != nil {
		zap.L().Warn("db: connect failed",
			zap.String("addr", c.addr), zap.Duration("elapsed", elapsed), zap.Error(err))
		return
	}
	zap.L().Info("db: opened connection",
		zap.String("addr", c.addr), zap.Duration("elapsed", elapsed))
}

func startConnectSegment(ctx context.Context) *newrelic.Segment {
	txn := newrelic.FromContext(ctx)
	if txn == nil {
		return nil
	}
	return txn.StartSegment(connectSegment)
}

func endConnectSegment(segment *newrelic.Segment) {
	if segment == nil {
		return
	}
	segment.End()
}

// newInstrumentedConnector builds the connector the pool opens against: the
// real MySQL connector, wrapped for connect timing, wrapped again by New Relic
// for query and exec segments.
func newInstrumentedConnector(mc *mysql.Config) (driver.Connector, error) {
	base, err := mysql.NewConnector(mc)
	if err != nil {
		return nil, err
	}
	timed := timedConnector{Connector: base, addr: mc.Addr}
	return newrelic.InstrumentSQLConnector(timed, datastoreSegmentBuilder(mc)), nil
}

// datastoreSegmentBuilder replicates nrmysql's own baseBuilder, which is
// unexported and therefore unreachable once the pool stops going through the
// registered "nrmysql" driver name.
//
// The Host / PortPathOrID / DatabaseName fields are not optional decoration:
// they are what New Relic facets datastore spans by, so leaving them empty
// would quietly strip host and database from every existing MySQL span in APM
// rather than only adding the new connect segment. ParseDSN is deliberately
// not set, unlike nrmysql's builder: InstrumentSQLConnector never calls it
// (only the driver-name path does), and there is no DSN to parse here because
// the target is already known from the config.
func datastoreSegmentBuilder(mc *mysql.Config) newrelic.SQLDriverSegmentBuilder {
	host, portPathOrID := datastoreTarget(mc)
	return newrelic.SQLDriverSegmentBuilder{
		BaseSegment: newrelic.DatastoreSegment{
			Product:      newrelic.DatastoreMySQL,
			Host:         host,
			PortPathOrID: portPathOrID,
			DatabaseName: mc.DBName,
		},
		ParseQuery: sqlparse.ParseQuery,
	}
}

// datastoreTarget splits mysql.Config.Addr the way nrmysql's parseConfig does,
// including its socket and cloudsql special cases and its fallback of
// reporting the whole address as the host when it does not split. Kept
// byte-for-byte equivalent on purpose: any divergence renames the datastore
// entity in New Relic and orphans the existing dashboards for it.
func datastoreTarget(mc *mysql.Config) (string, string) {
	switch mc.Net {
	case "unix", "unixgram", "unixpacket":
		return "localhost", mc.Addr
	case "cloudsql":
		return mc.Addr, ""
	}

	host, port, err := net.SplitHostPort(mc.Addr)
	if err != nil {
		return mc.Addr, ""
	}
	if host == "" {
		host = "localhost"
	}
	return host, port
}
