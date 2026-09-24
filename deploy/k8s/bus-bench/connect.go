// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/tlsenv"
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
	opts := []nats.Option{
		nats.UserInfo(os.Getenv("NATS_USER"), os.Getenv("NATS_PASSWORD")),
		nats.Timeout(15 * time.Second),
	}
	jwt, seed := os.Getenv("NATS_JWT"), os.Getenv("NATS_NKEY_SEED")
	if jwt != "" && seed != "" {
		opts = append(opts, nats.UserJWTAndSeed(jwt, seed))
	}
	return opts
}

func clientTLSConfig(pool *x509.CertPool) (*tls.Config, error) {
	cfg := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	pair, err := tlsenv.PairFromEnv("NATS_CLIENT_CERT_FILE", "NATS_CLIENT_KEY_FILE")
	if err != nil {
		return nil, err
	}
	if !pair.Configured() {
		return cfg, nil
	}
	cfg.GetClientCertificate = pair.GetClientCertificate
	return cfg, nil
}

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
		cfg, cerr := clientTLSConfig(pool)
		if cerr != nil {
			return nil, nil, cerr
		}
		opts = append(opts, nats.Secure(cfg))
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
		Name:               name,
		Subjects:           append([]string(nil), spec.Subjects...),
		Retention:          jsapi.LimitsPolicy,
		Storage:            jsapi.MemoryStorage,
		Replicas:           spec.Replicas,
		MaxAge:             spec.MaxAge,
		MaxBytes:           maxBytes,
		Duplicates:         10 * time.Second,
		AllowMsgSchedules:  false,
		AllowMsgTTL:        false,
		AllowAtomicPublish: spec.BatchPublish,
		AllowBatchPublish:  spec.BatchPublish,
	}
}
