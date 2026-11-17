// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql/driver"
	"net"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/newrelic/go-agent/v3/newrelic/sqlparse"
	"go.uber.org/zap"
)

const connectSegment = "db.connect"

type timedConnector struct {
	driver.Connector

	addr string
}

func (c timedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	segment := startConnectSegment(ctx)
	budget := connectBudget(ctx)
	started := time.Now()

	conn, err := c.Connector.Connect(ctx)

	endConnectSegment(segment)
	c.logConnect(time.Since(started), budget, err)
	return conn, err
}

func (c timedConnector) logConnect(elapsed time.Duration, budget zap.Field, err error) {
	if err != nil {
		zap.L().Warn("db: connect failed",
			zap.String("addr", c.addr), zap.Duration("elapsed", elapsed), budget, zap.Error(err))
		return
	}
	zap.L().Debug("db: opened connection",
		zap.String("addr", c.addr), zap.Duration("elapsed", elapsed), budget)
}

func connectBudget(ctx context.Context) zap.Field {
	deadline, ok := ctx.Deadline()
	if !ok {
		return zap.Bool("no_deadline", true)
	}
	return zap.Duration("budget", time.Until(deadline))
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

func newInstrumentedConnector(mc *mysql.Config) (driver.Connector, error) {
	base, err := mysql.NewConnector(mc)
	if err != nil {
		return nil, err
	}
	timed := timedConnector{Connector: base, addr: mc.Addr}
	return newrelic.InstrumentSQLConnector(timed, datastoreSegmentBuilder(mc)), nil
}

func datastoreSegmentBuilder(mc *mysql.Config) newrelic.SQLDriverSegmentBuilder {
	host, portPathOrID := datastoreTarget(mc)
	parse := sqlparse.ParseQuery
	if mc.MultiStatements {
		parse = parseMySQLPacket
	}
	return newrelic.SQLDriverSegmentBuilder{
		BaseSegment: newrelic.DatastoreSegment{
			Product:      newrelic.DatastoreMySQL,
			Host:         host,
			PortPathOrID: portPathOrID,
			DatabaseName: mc.DBName,
		},
		ParseQuery: parse,
	}
}

func parseMySQLPacket(segment *newrelic.DatastoreSegment, query string) {
	body := strings.TrimPrefix(query, "START TRANSACTION; ")
	sqlparse.ParseQuery(segment, body)
	if segment.Operation != "" {
		segment.RawQuery = query
	}
}

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
