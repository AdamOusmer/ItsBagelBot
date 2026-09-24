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

const connectSegment = "db.connect"

type timedConnector struct {
	driver.Connector

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
