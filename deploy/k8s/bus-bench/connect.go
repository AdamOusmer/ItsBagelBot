// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/pkg/bus"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

// Dialing and stream provisioning for the bench rig's management connection.

func loadCA() (*x509.CertPool, error) {
	caPEM := os.Getenv("NATS_CA_PEM")
	if caPEM == "" {
		return nil, nil
	}
	data := []byte(caPEM)
	if strings.HasPrefix(caPEM, "/") {
		b, rerr := os.ReadFile(caPEM)
		if rerr != nil {
			return nil, fmt.Errorf("read NATS_CA_PEM: %w", rerr)
		}
		data = b
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("NATS_CA_PEM contains no parseable PEM certificates")
	}
	return pool, nil
}

func baseConnectOptions() []nats.Option {
	return []nats.Option{
		nats.UserInfo(os.Getenv("NATS_USER"), os.Getenv("NATS_PASSWORD")),
		nats.Timeout(15 * time.Second),
	}
}

// clientTLSConfig wraps a CA pool with the client key pair the hub's
// verify:true listeners ask for. pkg/bus presents this pair on its own
// connections (connect.go), and the management connection must answer the same
// certificate request or the dial is refused.

// clientTLSConfig wraps a CA pool with the client key pair the hub's
// verify:true listeners ask for. pkg/bus presents this pair on its own
// connections (connect.go), and the management connection must answer the same
// certificate request or the dial is refused.
func clientTLSConfig(pool *x509.CertPool) *tls.Config {
	cfg := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	certFile, keyFile := os.Getenv("NATS_CLIENT_CERT_FILE"), os.Getenv("NATS_CLIENT_KEY_FILE")
	if certFile == "" || keyFile == "" {
		return cfg
	}
	cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		cert, cerr := tls.LoadX509KeyPair(certFile, keyFile)
		if cerr != nil {
			return nil, fmt.Errorf("load nats client key pair: %w", cerr)
		}
		return &cert, nil
	}
	return cfg
}

// jetStreamFor opens the management JetStream view on nc. jsapi.NewWithDomain
// refuses an empty domain; the fleet's JetStream plane lives in the "hub"
// domain (pkg/bus JSDomain default), which a direct-to-hub dial must name.

// jetStreamFor opens the management JetStream view on nc. jsapi.NewWithDomain
// refuses an empty domain; the fleet's JetStream plane lives in the "hub"
// domain (pkg/bus JSDomain default), which a direct-to-hub dial must name.
func jetStreamFor(nc *nats.Conn) (jsapi.JetStream, error) {
	domain := os.Getenv("NATS_JS_DOMAIN")
	if domain == "" {
		domain = "hub"
	}
	return jsapi.NewWithDomain(nc, domain)
}

func mgmtConnect(url string) (*nats.Conn, jsapi.JetStream, error) {
	opts := baseConnectOptions()
	pool, err := loadCA()
	if err != nil {
		return nil, nil, err
	}
	if pool != nil {
		opts = append(opts, nats.Secure(clientTLSConfig(pool)))
	}
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, nil, err
	}
	js, err := jetStreamFor(nc)
	if err != nil {
		nc.Close()
		return nil, nil, err
	}
	return nc, js, nil
}

func benchStreamConfig(name string, maxBytes int64) jsapi.StreamConfig {
	spec := bus.TwitchIngressRetryStream
	return jsapi.StreamConfig{
		Name:       name,
		Subjects:   append([]string(nil), spec.Subjects...),
		Retention:  jsapi.LimitsPolicy,
		Storage:    jsapi.MemoryStorage,
		Replicas:   spec.Replicas,
		MaxAge:     spec.MaxAge,
		MaxBytes:   maxBytes,
		Duplicates: 10 * time.Second,
		// Deliberately NOT spec.MsgSchedules. The high-volume ingress lane
		// (TwitchIngressStream) carries neither schedules nor TTLs; only the
		// retry stream needs them for its own retry mechanics. On the stream
		// leader both flags cost the serialized ingest+apply path ~0.6 s per
		// 10 s at 127k msg/s (getMessageSchedule runs unconditionally before
		// the allow check in jetstream_batching.go, memstore checks the TTL
		// wheel on every store), so a bench stream that copies them
		// under-reports the ingress lane by ~7%. Numbers taken on the retry
		// stream itself (the canary subject) stay conservative for that reason.
		AllowMsgSchedules:  false,
		AllowMsgTTL:        false,
		AllowAtomicPublish: spec.BatchPublish,
		AllowBatchPublish:  spec.BatchPublish,
	}
}
