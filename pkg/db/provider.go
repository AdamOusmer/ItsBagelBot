// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"entgo.io/ent/dialect"

	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/tlsenv"

	"github.com/go-sql-driver/mysql"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// The nrmysql integration is no longer imported. It exists only to register a
// driver name, and openPool now builds the pool from an instrumented
// connector instead (pkg/db/connector.go), which is what lets the connect
// handshake be timed at all. datastoreSegmentBuilder replicates the segment
// builder nrmysql installed, so query and exec segments are unchanged.

// Config carries everything needed to reach the MySQL schema owned by one
// service. Each service connects to its own schema and never to another's,
// keeping the isolation decided in ADR 0005.
type Config struct {
	Address  string // host:port
	Username string
	Password string
	Schema   string // the MySQL database owned by the calling service

	// MaxConns bounds the pool. Keep it small: every service shares the same
	// 8 GB HeatWave instance and MySQL connections are not free.
	MaxConns int

	// Monitor is the service's APM application, used only to report pool
	// statistics (pkg/db/stats.go). Optional: nil disables that sampler and
	// changes nothing else, which is what keeps local development and the
	// tests in this package working without an agent. Query, exec and connect
	// instrumentation does not go through here: those hang off the
	// transaction carried by the caller's context.
	Monitor *newrelic.Application
}

const (
	// defaultMaxConns is the per-pod ceiling applied twice: SetMaxOpenConns
	// in openPool (pkg/db/pool.go) and, independently, the DB_QUERY_CONCURRENCY
	// semaphore in pkg/db/gate.go, which falls back to this same constant
	// when unset. Both used to default to 4.
	//
	// Measured 2026-08-20: the managed MySQL HeatWave instance allows up to
	// 200 connections and peaked at 50 under real load; row lock waits are 0
	// and the whole dataset (<2 MB) is fully buffer-pool resident, so there
	// is no server-side contention this cap was protecting against. Meanwhile
	// only 9-12 sessions were open across 18 DB-backed pods each permitted 4
	// (72 possible) - the fleet was throttling itself against an idle
	// server, not the other way around.
	//
	// Raised to 8: at today's fleet size (18 DB-backed pods) that is
	// 18*8=144 connections if every pod maxed out simultaneously, 72% of the
	// 200 limit. Measured headroom on 2026-08-20: the server's
	// Max_used_connections high-water mark over 20.6 days of uptime was 50,
	// against a max_connections of 200, with Threads_connected sitting at 7-12
	// in steady state. 18 DB-backed pods x 8 = 144 is the new theoretical
	// ceiling, which fits, though it is a cap rather than an expectation --
	// actual usage is driven by concurrency and the app has never come close.
	//
	// (A 157 reading was briefly observed the same day and is NOT a production
	// signal: it was self-inflicted by an audit that opened ~250 bare TCP
	// probes to port 3306, visible as a matching +255 on Aborted_connects.
	// Recorded here because that number looked alarming and cost time once.)
	//
	// Revisit alongside the server's own max_connections if the fleet grows
	// much past ~20 DB-backed pods.
	//
	// This is a code default only, consulted when DB_MAX_OPEN_CONNS /
	// DB_QUERY_CONCURRENCY are unset. deploy/k8s/{users,transactions,modules,
	// commands,loyalty,notifications}.yaml all currently pin both to the
	// literal "4" as pod env vars, which override this constant - an operator
	// has to raise those Doppler/manifest values for this change to take
	// effect anywhere in production.
	defaultMaxConns = 8

	// connMaxLifetime forcibly recycles a connection regardless of use,
	// bounding how long any single connection can live (picks up server-side
	// config changes, cert rotation, etc. within that window, which is now 30
	// to 40 minutes once connMaxLifetimeJitter below is added). 30 minutes is
	// still a reasonable recycle cadence against the fleet's measured ~150
	// connections/day/service churn.
	connMaxLifetime = 30 * time.Minute

	// connMaxLifetimeJitter spreads the recycle clock across pods. openPool
	// (pkg/db/pool.go) draws one offset in [0, connMaxLifetimeJitter) per
	// process and hands SetConnMaxLifetime the sum, so a pod recycles
	// somewhere in 30 to 40 minutes instead of all pods recycling together.
	//
	// Measurement (production New Relic, 2026-09-07): normal query p50 is 1.4
	// to 4ms, but queries clump at 205 to 220ms across different pods inside
	// the same second, for example a projector prewarm fanout hitting commands
	// and modules simultaneously. 205ms is exactly one cold connect: 6 round
	// trips (TCP 1, TLS 2, MySQL auth 2, session-var SET 1) at the ~34ms RTT
	// of the public NLB path. They clump because every pod opened its pool
	// during the same rollout, so a fixed lifetime leaves the recycle clocks
	// phase-aligned fleet-wide and the whole fleet pays its handshakes in the
	// same second, repeatedly, until the next rollout re-aligns them somewhere
	// else.
	//
	// database/sql cannot do this itself: SetConnMaxLifetime takes one exact
	// duration and the package exposes no jitter knob, so the spread has to be
	// applied to the value handed in.
	//
	// 10 minutes is a third of the lifetime: wide enough that 18 pods land on
	// visibly different clocks, narrow enough that the effective 30 to 40
	// minute recycle window stays far under the wait_timeout assumed below.
	connMaxLifetimeJitter = 10 * time.Minute

	// connMaxIdleTime is 0, which tells database/sql never to close a
	// connection for having been idle. Only the lifetime clock recycles.
	//
	// It used to be 5 minutes, far shorter than the gap between requests on
	// most of these pods. Live evidence on 2026-08-20: only 9-12 MySQL
	// sessions open across 18 pods x 4 connections (72 possible), most pods
	// holding zero, and one connection observed idle at 253s and gone on the
	// next sample. A request landing on an empty pool pays TCP (1 RTT) + TLS
	// (2 RTT) + MySQL auth (2 RTT) + the session-var round trip (1 RTT) = 6
	// round trips before the query's own round trip even starts, ~205ms over
	// the public NLB path (see connMaxLifetimeJitter above). That is the
	// dominant DB latency in this system, not query execution (0.25 to 0.40ms
	// server-side, ~90% of wall time is transport).
	//
	// It was then set equal to connMaxLifetime, on the reasoning that
	// database/sql closes on whichever limit it hits first, so an idle timeout
	// equal to the lifetime can never fire strictly earlier. That worked, but
	// it had to be held equal by hand, and the lifetime is no longer a single
	// value: it is jittered per process, so "equal to the lifetime" is not
	// expressible as a constant any more. 0 states the intent directly,
	// disables the idle reaper outright and cannot drift out of sync with
	// anything.
	//
	// Assumption: OCI MySQL HeatWave has no custom wait_timeout configuration
	// applied, so it is running MySQL's own default, 28800s (8h). The 30 to 40
	// minute lifetime window is at least 12x under that, so a connection
	// reused right up against its lifetime is never at risk of the server
	// closing it first. If that assumption is wrong and an operator has set a
	// shorter wait_timeout, connMaxLifetime plus connMaxLifetimeJitter needs
	// to drop below it or connection reuse starts surfacing "MySQL server has
	// gone away" on a stale conn instead of paying the handshake. keepAlive
	// (pkg/db/keepalive.go) is the other half of this fix: it pings a small
	// floor of connections often enough that they never approach either
	// timeout in the first place.
	connMaxIdleTime = 0

	// dialTimeout, readTimeout and writeTimeout bound the network phases of a
	// connection. All three were previously unset, which in this driver means
	// no deadline at all.
	//
	// The DB path moved to a public OCI network load balancer on 2026-08-27.
	// That NLB drops an idle flow at around 300s without sending a RST to
	// either side, so a pooled connection can be blackholed while both ends
	// still believe it is open. With no read deadline, the next query on such
	// a connection blocks forever: not a slow request, a permanently stuck
	// goroutine holding a gate slot (pkg/db/gate.go) until the pod restarts.
	// keepAlive (pkg/db/keepalive.go) pings often enough that a pooled
	// connection should never reach the drop; these are the backstop for when
	// it does.
	//
	// 10s to dial covers the 6 round trip handshake (~205ms measured) with
	// enormous headroom while still failing inside a request context's own
	// patience. 30s to read or write is generous against a 1.4 to 4ms query
	// p50.
	//
	// Interaction with pkg/svcboot/databoot.AutoMigrate, which runs ent schema
	// creation at boot and is fatal on error: each migration statement now has
	// 30s to return. Ample for this dataset (<2 MB, fully buffer-pool
	// resident), but a future migration over a large table would hit the
	// deadline and fail the boot. Failing closed at boot is the deliberate
	// trade here: the alternative is a pod hanging in AutoMigrate forever with
	// no deadline and no signal. Raise these alongside such a migration rather
	// than removing them.
	dialTimeout  = 10 * time.Second
	readTimeout  = 30 * time.Second
	writeTimeout = 30 * time.Second
)

// NewDriver opens a bounded MySQL connection pool with the session settings
// pinned at the connection level (utf8mb4, READ COMMITTED, strict SQL mode,
// UTC) instead of relying on server defaults. The returned driver is meant to
// be handed to the service's own ent client via ent.Driver(...).
func NewDriver(cfg Config) (*entsql.Driver, error) {

	mc := newMySQLConfig(cfg)

	mode, err := resolveTLSMode()
	if err != nil {
		return nil, err
	}
	tlsName, err := registerTLS([]byte(env.Get("DB_CA_CERT", "")), mode, cfg.Address)
	if err != nil {
		return nil, err
	}
	mc.TLSConfig = tlsName

	pool, err := openPool(mc, cfg)
	if err != nil {
		return nil, err
	}

	return entsql.OpenDB(dialect.MySQL, pool), nil
}

// newMySQLConfig builds the driver config for one service's schema, minus
// TLS, which NewDriver attaches after resolving the mode. Kept separate from
// NewDriver so the field set can be asserted in tests without opening a pool
// or requiring a pinned CA in the environment.
//
// The timeouts here are honoured on the connector path openPool builds
// (mysql.NewConnector in pkg/db/connector.go): they are fields of
// mysql.Config, read by the driver itself, not DSN-only parameters.
func newMySQLConfig(cfg Config) *mysql.Config {
	mc := mysql.NewConfig()

	mc.Net = "tcp"
	mc.Addr = cfg.Address
	mc.User = cfg.Username
	mc.Passwd = cfg.Password
	mc.DBName = cfg.Schema

	mc.ParseTime = true
	mc.Loc = time.UTC
	mc.Collation = "utf8mb4_unicode_ci"
	mc.InterpolateParams = true // one round-trip per query instead of prepare+exec

	mc.Timeout = dialTimeout
	mc.ReadTimeout = readTimeout
	mc.WriteTimeout = writeTimeout

	mc.Params = map[string]string{
		"transaction_isolation": "'READ-COMMITTED'",
		"sql_mode":              "'STRICT_TRANS_TABLES,NO_ZERO_DATE,NO_ZERO_IN_DATE,ERROR_FOR_DIVISION_BY_ZERO'",
		"time_zone":             "'+00:00'",
	}

	return mc
}

// tlsConfigName is the key the registered tls.Config is stored under in the
// go-sql-driver registry and referenced from the DSN. The DB endpoint is the
// same managed HeatWave instance for every service, so one shared config is
// enough.
const tlsConfigName = "bagel-mysql"

// tlsSessionCacheSize is the number of resumable TLS sessions kept. One entry
// is used per server name, and there is exactly one endpoint, so 64 is far
// more than needed; it is the smallest round number that cannot become the
// limiting factor if a service ever talks to a second DB host.
const tlsSessionCacheSize = 64

// mysqlSessionCache is shared by both TLS modes so a reconnect can resume a
// previous session instead of running a full handshake.
//
// Measurement (production New Relic, 2026-09-07): a cold connect over the
// public NLB path is 6 round trips at ~34ms, ~205ms total, of which TLS is 2.
// Both tls.Config paths below were built with a nil ClientSessionCache, and a
// nil cache means Go stores no session tickets at all, so every single
// connect, including the ones keepAlive (pkg/db/keepalive.go) opens on a quiet
// pod, paid the full handshake. Resumption removes one of those round trips,
// roughly 34ms off every cold connect.
//
// Security is unchanged. VerifyConnection, which is what enforces the pinned
// CA in VERIFY_CA mode, was already chosen over VerifyPeerCertificate
// precisely because it also runs on resumed sessions (see verifyCATLSConfig
// below), so a resumed connection is checked against DB_CA_CERT exactly like a
// full one. The cache is process-local and holds session tickets only, never
// key material this package owns.
var mysqlSessionCache = tls.NewLRUClientSessionCache(tlsSessionCacheSize)

// TLS trust model for the managed MySQL HeatWave endpoint.
//
// History: on 2026-07-30 every DB-backed service lost connectivity for about
// 7.5 hours when handshakes started failing with "certificate signed by
// unknown authority" against the DB_CA_CERT pinned since 2026-07-02. That was
// read at the time as evidence that OCI reissues the managed endpoint CA on a
// roughly 28 day schedule (PR #517, "fix: survive OCI MySQL endpoint CA
// rotation"), and this package grew an in-band mechanism that would adopt
// whatever CA the server presented next, provided it reused the originally
// pinned key (an interim tightening over the first version, which matched on
// subject name alone).
//
// That belief does not hold up. It was a single data point (one observed
// change, 28 days after the CA was first pinned), and the CA re-pinned by
// hand right after the outage, still live today, has notBefore =
// 2026-07-30T11:52:17Z (the exact hour of the incident) and notAfter =
// 2029-07-29T11:52:17Z: a three year validity window. OCI does not mint a
// three year CA it plans to discard in 28 days. What rotated on 2026-07-30
// was almost certainly the server's LEAF certificate under a CA that had
// just been (re)issued, conflated in the original fix with the CA itself. The
// adoption mechanism this produced was also a genuine vulnerability: keyed on
// subject name, it would trust any self-signed certificate an attacker minted
// offline with the right CN; the SPKI based interim fix closed that specific
// hole but kept the underlying design, trusting whatever the server hands
// back on the wire, which is wrong regardless of how tightly the promotion
// check is written. It has been removed entirely here, not hardened further.
//
// Current state, DB_TLS_MODE=VERIFY_CA (the default): the endpoint presents
// OCI's service-defined, self-signed MySQL_Endpoint_CA. That certificate
// carries no SAN, so hostname identity cannot be checked, only the chain.
// DB_CA_CERT pins that CA. When OCI eventually reissues it for real, every
// service fails closed until an operator re-fetches the CA from the OCI
// console and updates DB_CA_CERT in Doppler, which restarts the consuming
// deployments (the Doppler operator rolls on secret change). That manual
// re-pin is the intended recovery path now, not an automatic one:
// warnIfPinnedCANearExpiry logs at error level starting 30 days before
// DB_CA_CERT's notAfter so the deadline surfaces before it becomes an outage.
//
// Target state, DB_TLS_MODE=VERIFY_IDENTITY: OCI MySQL HeatWave DB Systems
// support bringing your own certificate (BYOC) through the OCI Certificates
// Service, letting the DB System present a certificate signed by a CA the
// operator controls, with a SAN for the DB System's endpoint. Once that
// switch is made, VERIFY_IDENTITY turns on plain Go verification (RootCAs
// plus ServerName, no custom hook, no InsecureSkipVerify): strictly stronger
// than VERIFY_CA because it also binds the server's identity, not only the
// chain.
//
// Cutover sequence (do this in order; associating a certificate restarts the
// DB System, so a brief connection gap is expected regardless of ordering,
// the goal is only to keep that gap short):
//  1. In the OCI Certificates Service, create or reuse a CA and issue a leaf
//     certificate for the DB System's endpoint. Give it a SAN matching
//     whatever DB_ADDR's host portion will be after the switch: an IP SAN
//     for the DB System's private IP, or a DNS SAN for its FQDN if DB_ADDR
//     is also moving to that FQDN (ServerName is derived from DB_ADDR's
//     host, see registerTLS). Export the CA certificate, not the leaf.
//  2. Associate the certificate with the DB System (OCI console, or
//     UpdateDbSystem with secureConnections set to the certificate OCID).
//     This restarts the DB System.
//  3. Update DB_CA_CERT in Doppler to the CA from step 1 and set
//     DB_TLS_MODE=VERIFY_IDENTITY, for every project that pins it (users,
//     commands, loyalty, modules, notifications, transactions). Do this
//     immediately after step 2 so the window where the DB System's live
//     certificate and a service's trust configuration disagree stays short.
//  4. Confirm each service reconnects (dashboard login and the admin console
//     are the fastest smoke test) before considering the migration done.
//
// Rotating the leaf afterward is steps 1 and 2 again over the same CA, with
// no Doppler change and no service restart: the VERIFY_CA steady state
// today, except the identity check now also applies.
type tlsMode string

const (
	// tlsModeVerifyCA verifies the presented chain against the pinned CA but
	// not the server's identity (no SAN match), matching MySQL's own
	// VERIFY_CA ssl-mode.
	tlsModeVerifyCA tlsMode = "VERIFY_CA"

	// tlsModeVerifyIdentity verifies the presented chain against the pinned
	// CA and that the certificate's SAN matches the server host, matching
	// MySQL's own VERIFY_IDENTITY ssl-mode.
	tlsModeVerifyIdentity tlsMode = "VERIFY_IDENTITY"

	tlsModeEnvVar = "DB_TLS_MODE"
)

// resolveTLSMode reads DB_TLS_MODE, defaulting to VERIFY_CA: that is what
// matches the endpoint's actual certificate today, so a service that never
// sets the variable keeps behaving exactly as it does now, and a deploy of
// this change is a no-op until an operator flips it. An unrecognised value
// fails closed rather than silently picking a mode.
func resolveTLSMode() (tlsMode, error) {
	switch mode := tlsMode(env.Get(tlsModeEnvVar, string(tlsModeVerifyCA))); mode {
	case tlsModeVerifyCA, tlsModeVerifyIdentity:
		return mode, nil
	default:
		return "", fmt.Errorf("db: %s must be %q or %q, got %q",
			tlsModeEnvVar, tlsModeVerifyCA, tlsModeVerifyIdentity, mode)
	}
}

// registerTLS builds and registers the TLS config used for every MySQL
// connection so traffic to the managed HeatWave endpoint is always encrypted
// and authenticated. See the tlsMode doc comment above for what each mode
// checks and the migration path between them.
//
// DB_CA_CERT must hold the trusted CA (PEM); connections fail closed when it
// is absent or invalid, regardless of mode.
func registerTLS(caPEM []byte, mode tlsMode, addr string) (string, error) {
	cfg, err := newMySQLTLSConfig(caPEM, mode, addr)
	if err != nil {
		return "", err
	}

	if err := mysql.RegisterTLSConfig(tlsConfigName, cfg); err != nil {
		return "", err
	}
	return tlsConfigName, nil
}

// newMySQLTLSConfig parses the pinned CA once and builds the tls.Config for
// the requested mode. addr is only consulted for VERIFY_IDENTITY, to derive
// the ServerName Go checks the presented certificate's SAN against.
func newMySQLTLSConfig(caPEM []byte, mode tlsMode, addr string) (*tls.Config, error) {
	pinned, err := parsePinnedCA(caPEM)
	if err != nil {
		return nil, err
	}
	warnIfPinnedCANearExpiry(pinned)

	clientCerts, err := loadClientKeyPair()
	if err != nil {
		return nil, err
	}

	if mode != tlsModeVerifyIdentity {
		cfg := verifyCATLSConfig(pinned)
		cfg.Certificates = clientCerts
		return cfg, nil
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("db: DB_ADDR must be host:port for VERIFY_IDENTITY, got %q: %w", addr, err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(pinned)
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		RootCAs:            roots,
		ServerName:         host,
		Certificates:       clientCerts,
		ClientSessionCache: mysqlSessionCache,
	}, nil
}

const (
	clientCertEnvVar = tlsenv.Var("DB_CLIENT_CERT")
	clientKeyEnvVar  = tlsenv.Var("DB_CLIENT_KEY")
)

// loadClientKeyPair reads the optional mTLS client identity presented to the
// server. The MySQL accounts are moving to REQUIRE X509 (2026-08-27, client
// certificates issued by the ItsBagelBot HeatWave Root CA in the OCI
// Certificates service), so a service without a client certificate will be
// refused by the server even with the right password once its account is
// flipped. Both variables unset is still accepted deliberately: it keeps this
// change deployable before the account flip, and keeps dev environments
// against permissive local MySQL working.
//
// Half-configured (one of the two set) fails closed instead of silently
// connecting certificate-less: that state is always a Doppler mistake, and
// the server-side REQUIRE X509 refusal it would otherwise surface as is far
// harder to diagnose than this error.
func loadClientKeyPair() ([]tls.Certificate, error) {

	// Only the both-or-neither gate is shared with tlsenv, not the loading:
	// these two variables carry the PEM bodies themselves, not paths to them
	// (HeatWave client identities come from Doppler, there is no Secret volume
	// on this path), so Pair's file-based accessors and closures do not apply.
	// The gate is the part that was worth having one wording of.
	pair, err := tlsenv.PairFromEnv(clientCertEnvVar, clientKeyEnvVar)
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	if !pair.Configured() {
		return nil, nil
	}

	certPEM := strings.TrimSpace(env.Get(string(clientCertEnvVar), ""))
	keyPEM := strings.TrimSpace(env.Get(string(clientKeyEnvVar), ""))
	keyPair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("db: parsing %s/%s client key pair: %w",
			clientCertEnvVar, clientKeyEnvVar, err)
	}
	return []tls.Certificate{keyPair}, nil
}

func parsePinnedCA(caPEM []byte) (*x509.Certificate, error) {
	caPEM = bytes.TrimSpace(caPEM)
	if len(caPEM) == 0 {
		return nil, fmt.Errorf("db: DB_CA_CERT is required")
	}

	block, _ := pem.Decode(caPEM)
	if block == nil {
		return nil, fmt.Errorf("db: DB_CA_CERT did not contain a valid PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("db: DB_CA_CERT did not contain a valid PEM certificate")
	}
	return cert, nil
}

// caExpiryWarningWindow is how far ahead of DB_CA_CERT's notAfter the pin
// starts logging at error level. The pin governing this connection is now a
// long-lived CA (see the tlsMode doc comment above), which is exactly the
// kind of deadline that is easy to forget until it becomes an outage; 30 days
// gives an operator time to re-fetch and re-pin before that happens.
const caExpiryWarningWindow = 30 * 24 * time.Hour

// warnIfPinnedCANearExpiry logs at error level, not warn: an expiring pin is
// a coming fleet-wide outage, and error level is what reaches Fluent Bit and
// New Relic alerting without depending on anyone reading info logs.
func warnIfPinnedCANearExpiry(pinned *x509.Certificate) {
	remaining := time.Until(pinned.NotAfter)
	if remaining > caExpiryWarningWindow {
		return
	}
	zap.L().Error("db: pinned MySQL endpoint CA (DB_CA_CERT) is close to expiry, "+
		"re-fetch the current CA (OCI console for the service-defined CA, or the "+
		"OCI Certificates Service certificate for a BYOC CA) and update DB_CA_CERT in Doppler",
		zap.Time("notAfter", pinned.NotAfter),
		zap.Duration("remaining", remaining),
	)
}

// verifyCATLSConfig implements MySQL's VERIFY_CA semantics: the presented
// chain must verify against the pinned CA, no hostname check. InsecureSkipVerify
// paired with a custom VerifyConnection is what implements that combination
// in Go's tls package; VerifyConnection, rather than VerifyPeerCertificate, is
// used because it also runs when a TLS session is resumed.
func verifyCATLSConfig(pinned *x509.Certificate) *tls.Config {
	roots := x509.NewCertPool()
	roots.AddCert(pinned)

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// codeql[go/disabled-certificate-check] -- VERIFY_CA mode: the OCI
		// service-defined MySQL_Endpoint_CA signs a certificate with no SAN,
		// so hostname identity cannot be checked. VerifyConnection below
		// still requires the presented chain to verify against the pinned
		// DB_CA_CERT and fails closed otherwise. Nothing the server presents
		// is ever trusted beyond that single check: there is no fallback and
		// no in-band promotion of an alternate CA.
		InsecureSkipVerify: true,
		VerifyConnection:   verifyAgainstPinned(roots),
		ClientSessionCache: mysqlSessionCache,
	}
}

// verifyAgainstPinned returns a VerifyConnection callback that checks the
// presented chain against roots and nothing else.
func verifyAgainstPinned(roots *x509.CertPool) func(tls.ConnectionState) error {
	return func(state tls.ConnectionState) error {
		certs := state.PeerCertificates
		if len(certs) == 0 {
			return fmt.Errorf("db: server presented no certificate")
		}
		if err := verifyChain(certs, roots); err != nil {
			return fmt.Errorf("db: server certificate does not chain to the pinned "+
				"DB_CA_CERT, re-fetch the endpoint CA from the OCI console and update "+
				"DB_CA_CERT in Doppler: %w", err)
		}
		return nil
	}
}

func verifyChain(certs []*x509.Certificate, roots *x509.CertPool) error {
	intermediates := x509.NewCertPool()
	for _, c := range certs[1:] {
		intermediates.AddCert(c)
	}
	_, err := certs[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates})
	return err
}
