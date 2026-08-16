// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package bus

import (
	"crypto/tls"
	"crypto/x509"
	"regexp"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/pkg/env"
)

// The fleet runs two NATS planes on two credential sets:
//
//   - the RPC + cache-invalidate plane (core request/reply), authenticated with
//     the service's own per-service account via NATS_RPC_USER / NATS_RPC_PASSWORD;
//   - the durable JetStream event plane, authenticated with the shared BUS
//     account via NATS_USER / NATS_PASSWORD.
//
// The RPC + cache plane stays on the leaf cluster. The nats-leaf Service prefers
// the same-node endpoint and falls back to another leaf when needed; it never
// spills RPC onto the stream hub. The JetStream plane dials the hub directly (busURL reads
// NATS_HUB_URL): the durable streams live on the hub, so routing JetStream
// through the leaf is only an extra forwarding hop (the leaf runs no JetStream).
// This mirrors the console lib's rpc/bus split in
// console/shared/lib/server/nats.ts.
//
// COUPLED TO the broker configs — this file is the client half of the topology
// declared by deploy/k8s/nats-leaf-server.conf (leaf tier: plane split, TLS
// posture, keepalives, server_name prefix; see the header there for the full
// list) and deploy/k8s/nats-server.conf (hub). Change either side with the
// other open.

// JSDomain is the JetStream domain the fleet's streams live in. Clients dial the
// leaf (whose own JetStream domain is "leaf"), so every JetStream context must be
// domain-qualified to reach the authoritative hub streams.
func JSDomain() string { return env.Get("NATS_JS_DOMAIN", "hub") }

// clientName is the name a connection reports to NATS monitoring, and endpoint
// is a NATS URL or comma-separated server list. Both are distinct types so a
// call site cannot transpose them into a connection that dials its own name.
type clientName string

type endpoint string

// serverList returns the RPC endpoint. In split-plane production the leaf
// Service itself supplies same-node preference and cross-node leaf failover;
// adding the hub here would violate the RPC-only leaf / streams-only hub split.
// With no split configured, override keeps local development on one server.
func serverList(override endpoint) string {
	leaf := env.Get("NATS_LEAF_URL", "")
	if leaf != "" {
		return leaf
	}
	return string(override)
}

// connectionIdentity is who a connection dials as: the client name NATS
// monitoring reports it under, and the account credentials it authenticates
// with. The three were a positional string triple, where transposing the user
// and the password authenticates as the wrong account and the broker's rejection
// is the only place that shows.
type connectionIdentity struct {
	name string
	user string
	pass string
}

// baseOptions are shared by every connection the fleet opens, core or
// JetStream: endless reconnects, a stable endpoint that is never shuffled, a
// client name for monitoring, and the supplied credentials.
// Local development runs against an open server, so empty credentials are fine;
// the broker is the one enforcing them.
func baseOptions(identity connectionIdentity) []nats.Option {
	opts := []nats.Option{
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		// 32 MB buffer so publishes during a reconnect are not lost. Raised from
		// 8 MB for the 150k firehose: a hub roll can briefly disconnect the async
		// publisher even with the streams at R3 (the pod's own connection goes
		// with the member it was pinned to), and at 150k/s an 8 MB buffer fills
		// in well under a second, dropping events the dedup window can't recover.
		nats.ReconnectBufSize(32 * 1024 * 1024),
		nats.PingInterval(20 * time.Second),
		nats.MaxPingsOutstanding(3),
		nats.Timeout(15 * time.Second),
		nats.RetryOnFailedConnect(true),
		// Honor serverList order on the initial dial and on every reconnect; the
		// default shuffles the pool and would let a client pin the hub.
		nats.DontRandomize(),
		// Keep reconnecting through authorization errors. The default aborts the
		// connection for good after two consecutive auth failures against the
		// same server, which permanently strands a pod when the broker's account
		// env lags a credential rotation (readyz stays 503 but healthz keeps the
		// container alive, so it never restarts on its own).
		nats.IgnoreAuthErrorAbort(),
		// Without this, an ACL mistake is invisible. The server reports a
		// permissions violation out-of-band on the -ERR line: the publish call
		// that caused it has already returned nil, the message is simply never
		// stored, and nats.go's only remaining channel for it is this callback.
		// A missing $JS.FC grant wedged both hot ingress lanes with no log line
		// anywhere in the fleet, which is the failure this option exists to make
		// impossible to repeat.
		nats.ErrorHandler(logAsyncError),
	}

	// Verify the broker's TLS server cert against the fleet CA when one is
	// configured (see tlsSecureOption), the wire encryption now that NATS is out of
	// the Linkerd mesh.
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

// violatedSubjectPattern extracts the subject out of the server's transient
// error text ("Permissions Violation for Publish to \"$JS.FC.X.Y.abcd\"").
// nats.go passes a nil *Subscription for publish violations, so the error string
// is the only place the subject appears.
var violatedSubjectPattern = regexp.MustCompile(`"([^"]+)"`)

// logAsyncError reports the errors the NATS client can only deliver
// out-of-band: permission violations on publish or subscribe, slow-consumer
// drops, and the subscription limits. It logs at Error because every one of them
// means a message the caller believes it sent or received did not happen, and
// the caller has already been told nothing.
//
// It uses the global logger on purpose: this is a connection option shared by
// every bus connection in the process, and pkg/logger installs the fleet logger
// as the global at startup, so services get their real logger without threading
// one through every call site.
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

// tlsSecureOption returns a nats.Secure option that verifies the broker's server
// cert against the fleet CA (NATS_CA_PEM, distributed by trust-manager as the
// fleet-ca ConfigMap), or nil when no CA is configured — local dev against a
// plaintext server stays plaintext. Server-auth only: the client still
// authenticates with its bcrypt user/password, not a client cert.
func tlsSecureOption() nats.Option {
	caPEM := env.Get("NATS_CA_PEM", "")
	if caPEM == "" {
		return nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil
	}
	return nats.Secure(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12})
}

// rpcOptions authenticate the per-service account on the RPC plane. The creds
// fall back to NATS_USER/NATS_PASSWORD so local dev and the phased rollout (RPC
// creds not yet provisioned) keep working against the shared user.
func rpcOptions(name clientName) []nats.Option {
	opts := baseOptions(connectionIdentity{
		name: string(name),
		user: env.Get("NATS_RPC_USER", env.Get("NATS_USER", "")),
		pass: env.Get("NATS_RPC_PASSWORD", env.Get("NATS_PASSWORD", "")),
	})
	// Leaf failback applies ONLY to the RPC plane, which is leaf-first: recycle a
	// connection displaced onto a fallback leaf back to the node-local leaf once it
	// recovers. The BUS plane (busOptions) dials the hub directly (NATS_HUB_URL),
	// whose server_name is "nats-N", never "<node>--…" — failback would treat it as
	// permanently displaced and ForceReconnect it every interval (~90s), churning
	// the JetStream consumers. That is what broke outgress -> Twitch delivery after
	// the BUS plane moved hub-direct.
	if option := leafFailbackOption(); option != nil {
		opts = append(opts, option)
	}
	return opts
}

// busOptions authenticate the shared BUS account on the JetStream plane.
func busOptions(name clientName) []nats.Option {
	return baseOptions(connectionIdentity{
		name: string(name),
		user: env.Get("NATS_USER", ""),
		pass: env.Get("NATS_PASSWORD", ""),
	})
}

// Connect opens a core NATS connection for request-reply RPC and ephemeral
// subscriptions on the per-service account through the leaf tier. name identifies the
// service in NATS monitoring.
func Connect(url string, name string) (*nats.Conn, error) {
	return nats.Connect(serverList(endpoint(url)), rpcOptions(clientName(name))...)
}

// jsDomainOption is the JetStream connect option that targets the hub domain.
// Exposed as a slice so callers can splice it into a
// JetStreamConfig.ConnectOptions / nc.JetStream call.
func jsDomainOption() []nats.JSOpt {
	return []nats.JSOpt{nats.Domain(JSDomain())}
}

// RPCURL returns the core NATS endpoint used for request/reply traffic. It
// intentionally falls back to the durable bus URL so local development and old
// deployments keep working, while production can point RPC at a node-local leaf
// without moving JetStream publisher/subscriber traffic.
func RPCURL(busURL string) string {
	return env.Get("NATS_RPC_URL", busURL)
}

// busURL resolves the JetStream-plane endpoint. The durable streams live on the
// hub, so for JetStream the node-local leaf is only an extra forwarding hop:
// dial the hub directly when NATS_HUB_URL is set (mirroring busServerList in
// console/shared/lib/server/nats.ts). Falls back to the configured endpoint
// when no hub is configured (local dev / single-endpoint deploys). RPC stays
// on the leaf via RPCURL/serverList.
func busURL(url endpoint) string {
	if hub := env.Get("NATS_HUB_URL", ""); hub != "" {
		return hub
	}
	return serverList(url)
}

// busPublishURL resolves the endpoint for the asynchronous publisher pool.
// Production sets NATS_HUB_PUBLISH_URL to the PreferSameNode hub Service: each
// pod performs client TLS/socket/batch work on its local NATS member, then NATS
// routes the commit to the stream's RAFT leader. Consumers may still pin
// NATS_HUB_URL to the member leading their stream, so publish and consume
// locality remain independent. Local development falls back to busURL.
func busPublishURL(url endpoint) string {
	if publish := env.Get("NATS_HUB_PUBLISH_URL", ""); publish != "" {
		return publish
	}
	return busURL(url)
}
