// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sync"
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
//
// mTLS: both the hub's client listener (4222, nats-server.conf) and the
// leaf's client listener (4223, nats-leaf-server.conf) require a client
// certificate once their tls.verify:true lands. clientCertFile/clientKeyFile
// below is the ONE fixed mount path every service gets, fleet-wide, from the
// kustomize patch in deploy/k8s/kustomization.yaml (the shared
// nats-bus-client-tls Secret, issued in deploy/infra/pki/certificates.yaml)
// — not a per-service env var. tlsSecureOption is shared by rpcOptions and
// busOptions (in fact by every nats.Connect call site in this package:
// bus.go, batch_publisher.go, flow_delivery.go, provision.go,
// pull_consumer.go all fall through busOptions/rpcOptions), so this one
// function is the entire Go-side chokepoint — pkg/svcboot's MustNATS has no
// TLS logic of its own, it composes bus.Connect/NewPublisher/NewSubscriber
// like every other caller. Account isolation (bcrypt user/password ->
// account, auth.conf) is unchanged: this is chain-of-trust verify:true, not
// verify_and_map, so it layers under the existing auth rather than replacing
// any part of it.
//
// ROTATION: the client cert is loaded via a tls.Config.GetClientCertificate
// callback (globalClientCert/clientCertCache below), not a one-time static
// tls.Config.Certificates value, specifically so a cert-manager rotation
// gets picked up on the next handshake instead of failing at expiry with no
// warning in between. See clientCertCache's doc comment for the mechanism
// and clientCertFile/clientKeyFile's for why it is a fixed path.

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

// clientCertFile / clientKeyFile are the fixed mount path every service's
// manifest gets via the fleet-wide kustomize patch in
// deploy/k8s/kustomization.yaml (the nats-bus-client-tls Secret, issued in
// deploy/infra/pki/certificates.yaml). Fixed and non-configurable per
// service on purpose: a per-service env var meant every manifest had to
// remember to set it, and a forgotten one connected with no client cert and
// only failed once the server's verify:true landed — silent until then. One
// baked-in path means the only way to not present a cert is to not mount
// the Secret, which the fleet-wide patch does unconditionally for every
// Deployment/CronJob that dials NATS.
const (
	clientCertFile = "/etc/nats/client-certs/tls.crt"
	clientKeyFile  = "/etc/nats/client-certs/tls.key"
)

// errNoClientCert marks "no cert at the mount path" as distinct from "a cert
// is there but broken." clientCertCache.get and tlsSecureOption treat the two
// differently: absent means the fleet-wide mount has not landed on this pod
// (or has been removed under it), broken means a real misconfiguration.
var errNoClientCert = errors.New("no client certificate at mount path")

// clientCertCache holds this process's parsed mTLS client certificate plus
// the mtimes it was parsed from, so repeated handshakes do not each pay a
// disk read + X509 parse — only a changed mtime does.
//
// This exists instead of a one-time load into tls.Config.Certificates
// because a static value is captured once, at connection-option
// construction, and never revisited: cert-manager renews nats-bus-client-tls
// at day 75 of its 90-day lifetime, and a process that loaded the cert once
// at startup would keep presenting the DAY-0 cert until day 90, then fail —
// with nothing failing in between to warn anyone. That is a dated,
// fleet-wide outage on a predictable schedule. tls.Config.GetClientCertificate
// is invoked on every handshake (including nats.go's own automatic
// reconnects), so wiring the cache's get method in as that callback (see
// tlsSecureOption) makes every reconnect after day 75 pick up the rotated
// file automatically — no watcher, no sidecar, no restart, no forced
// reconnection logic. An already-open connection is not re-validated
// mid-session, so it keeps working on the old cert until it naturally drops
// (a hub/leaf roll, a network blip); since the old cert stays valid through
// day 90 and renewal lands at day 75, any reconnect in that 15-day window is
// enough.
type clientCertCache struct {
	mu     sync.Mutex
	cert   *tls.Certificate
	certAt time.Time
	keyAt  time.Time
}

// get returns the parsed client certificate for certFile/keyFile, reusing
// the cached parse when neither file's mtime has moved since the last call.
// Returns errNoClientCert when the files are not there. A reload that fails
// while a previous cert is already cached logs the failure and keeps serving
// that previous cert — a transient or partial write to the mounted Secret
// should not take an otherwise-healthy process's next handshake down.
func (c *clientCertCache) get(certFile, keyFile string) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	certInfo, err := os.Stat(certFile)
	if err != nil {
		if c.cert != nil {
			return c.cert, nil
		}
		return nil, errNoClientCert
	}
	keyInfo, err := os.Stat(keyFile)
	if err != nil {
		if c.cert != nil {
			return c.cert, nil
		}
		return nil, errNoClientCert
	}

	if c.cert != nil && certInfo.ModTime().Equal(c.certAt) && keyInfo.ModTime().Equal(c.keyAt) {
		return c.cert, nil
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		if c.cert != nil {
			zap.L().Error("nats client cert reload failed, keeping previous cert",
				zap.String("cert_file", certFile), zap.Error(err))
			return c.cert, nil
		}
		return nil, fmt.Errorf("load client cert %s: %w", certFile, err)
	}

	c.cert = &cert
	c.certAt = certInfo.ModTime()
	c.keyAt = keyInfo.ModTime()
	return c.cert, nil
}

// globalClientCert is the one cache every connection in the process shares —
// they all present the same fleet-wide cert at the same fixed path, so there
// is nothing to gain from a cache per connection.
var globalClientCert clientCertCache

// tlsSecureOption returns a nats.Secure option that verifies the broker's server
// cert against the fleet CA (NATS_CA_PEM, distributed by trust-manager as the
// fleet-ca ConfigMap), or nil when no CA is configured — local dev against a
// plaintext server stays plaintext. Also presents this service's own client
// certificate via GetClientCertificate (globalClientCert, rotation-safe —
// see its doc comment), which is REQUIRED whenever TLS itself is on: the
// hub's 4222 and leaf's 4223 listeners set verify:true, so a connection with
// no client cert fails the TLS handshake before auth.conf's bcrypt
// user/password check ever runs.
//
// The eager get() call below (once per option-construction call, i.e. once
// per process start and once per nats.go reconnect-loop entry) is what makes
// a missing/unreadable cert a boot-time crash in one place rather than a
// connect loop that just quietly never presents one — GetClientCertificate
// alone would only surface the failure inside a handshake, indistinguishable
// from any other connect failure in the logs.
func tlsSecureOption() nats.Option {
	caPEM := env.Get("NATS_CA_PEM", "")
	if caPEM == "" {
		return nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil
	}

	if _, err := globalClientCert.get(clientCertFile, clientKeyFile); err != nil {
		zap.L().Fatal("nats mTLS client certificate required but unavailable",
			zap.String("cert_file", clientCertFile), zap.Error(err))
	}

	return nats.Secure(&tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return globalClientCert.get(clientCertFile, clientKeyFile)
		},
	})
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
