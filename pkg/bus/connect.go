// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"crypto/tls"
	"crypto/x509"
	"regexp"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/tlsenv"
)

// Connection planes are mirrored in web/kit/lib/server/nats.ts; change both together.

func JSDomain() string { return env.Get("NATS_JS_DOMAIN", "hub") }

type clientName string

type endpoint string

func serverList(override endpoint) string {
	leaf := env.Get("NATS_LEAF_URL", "")
	if leaf != "" {
		return leaf
	}
	return string(override)
}

type connectionIdentity struct {
	name string
	user string
	pass string
}

func baseOptions(identity connectionIdentity) []nats.Option {
	opts := []nats.Option{
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.ReconnectBufSize(32 * 1024 * 1024),
		nats.WriteBufferSize(writeBufferSize()),

		nats.PingInterval(20 * time.Second),
		nats.MaxPingsOutstanding(3),
		nats.Timeout(15 * time.Second),
		nats.RetryOnFailedConnect(true),
		nats.DontRandomize(),
		nats.IgnoreAuthErrorAbort(),
		nats.ErrorHandler(logAsyncError),
	}

	if option := tlsSecureOption(); option != nil {
		opts = append(opts, option)
	}

	if identity.name != "" {
		opts = append(opts, nats.Name(identity.name))
	}

	if identity.user != "" {
		opts = append(opts, nats.UserInfo(identity.user, identity.pass))
	}

	return opts
}

var violatedSubjectPattern = regexp.MustCompile(`"([^"]+)"`)

func logAsyncError(_ *nats.Conn, sub *nats.Subscription, err error) {
	subject := ""
	if sub != nil {
		subject = sub.Subject
	}
	if subject == "" && err != nil {
		if match := violatedSubjectPattern.FindStringSubmatch(err.Error()); len(match) == 2 {
			subject = match[1]
		}
	}
	zap.L().Error("nats asynchronous error",
		zap.String("subject", subject),
		zap.Error(err))
}

func failedTLSOption(err error) nats.Option {
	return func(*nats.Options) error { return err }
}

func tlsSecureOption() nats.Option {
	caPEM := env.Get("NATS_CA_PEM", "")
	if caPEM == "" {
		return nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil
	}

	cfg := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	pair, err := tlsenv.PairFromEnv("NATS_CLIENT_CERT_FILE", "NATS_CLIENT_KEY_FILE")
	if err != nil {
		return failedTLSOption(err)
	}
	if pair.Configured() {
		cfg.GetClientCertificate = pair.GetClientCertificate
	}
	return nats.Secure(cfg)
}

func rpcOptions(name clientName) []nats.Option {
	opts := baseOptions(connectionIdentity{
		name: string(name),
		user: env.Get("NATS_RPC_USER", env.Get("NATS_USER", "")),
		pass: env.Get("NATS_RPC_PASSWORD", env.Get("NATS_PASSWORD", "")),
	})
	// Failback only on the leaf RPC plane: on the BUS plane it would force a reconnect every interval.
	if option := leafFailbackOption(); option != nil {
		opts = append(opts, option)
	}
	return opts
}

func busOptions(name clientName) []nats.Option {
	return baseOptions(connectionIdentity{
		name: string(name),
		user: env.Get("NATS_USER", ""),
		pass: env.Get("NATS_PASSWORD", ""),
	})
}

func Connect(url string, name string) (*nats.Conn, error) {
	return nats.Connect(serverList(endpoint(url)), rpcOptions(clientName(name))...)
}

func jsDomainOption() []nats.JSOpt {
	return []nats.JSOpt{nats.Domain(JSDomain())}
}

func RPCURL(busURL string) string {
	return env.Get("NATS_RPC_URL", busURL)
}

func busURL(url endpoint) string {
	if hub := env.Get("NATS_HUB_URL", ""); hub != "" {
		return hub
	}
	return serverList(url)
}

func busPublishURL(url endpoint) string {
	if publish := env.Get("NATS_HUB_PUBLISH_URL", ""); publish != "" {
		return publish
	}
	return busURL(url)
}

func writeBufferSize() int {
	return max(env.GetInt("NATS_WRITE_BUFFER_SIZE", 32*1024), 32*1024)
}
