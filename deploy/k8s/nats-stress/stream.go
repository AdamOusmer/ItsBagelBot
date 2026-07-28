package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

const (
	// stressStreamName and stressSubjectPrefix are pinned by the broker ACL, not
	// chosen here: admin_bus holds STREAM.CREATE/UPDATE/DELETE/LEADER.STEPDOWN and
	// $JS.FC only for this exact stream name, and worker_bus's publish grant
	// reaches these subjects only because they sit under twitch.outgress.>.
	// Renaming either without editing deploy/k8s/nats-auth.conf produces a rig
	// that authenticates, publishes nothing, and reports zero as if it were a
	// measurement (see deploy/k8s/nats-auth.conf and nats_auth_test.go).
	stressStreamName    = "R3_SHADOW_BENCH"
	stressSubjectPrefix = "twitch.outgress.bench.r3."

	// stressHotSubject is the single hot lane every publisher lane writes to and
	// the consumer filters on. One subject on purpose: MaxMsgsPerSubject is the
	// eviction budget being measured, and spreading the load over several
	// subjects would quietly multiply it.
	stressHotSubject = stressSubjectPrefix + "hot"

	// streamOpTimeout bounds the rig's own control calls (delete, info, consumer
	// list). It is short because none of them are on a measured path and a hung
	// control call must not be mistaken for a stalled lane.
	streamOpTimeout = 15 * time.Second
)

// stressStream is the shadow stream's desired state, deliberately built out of
// the production catalog type so the rig converges through exactly the machinery
// (pkg/bus/provision.go on the modern JetStream API) that provisions
// TWITCH_INGRESS. A hand-rolled jsapi.StreamConfig would drift from the catalog's
// server-normalisation rules and reconcile forever, and — worse for the numbers —
// would let the two batch capabilities be forgotten, which turns every fast-wire
// cohort into a rejection.
//
// The limits mirror TWITCH_INGRESS because the point of the run is the ingress
// lane's operating point: R3 so the RAFT commit cost is real, memory-backed so
// the per-event disk write is out of the way, 1 GiB stream cap and 400k
// per-subject cap so eviction happens where production would evict.
func stressStream() bus.StreamSpec {
	return bus.StreamSpec{
		Name:     stressStreamName,
		Subjects: []string{stressSubjectPrefix + ">"},
		MaxAge:   5 * time.Minute,
		MaxBytes: 1 << 30,
		// The consumer's lag budget in messages. At 100k/s this is four seconds of
		// buffer before oldest-first eviction starts, which is what makes a flat
		// lag curve a meaningful soak assertion rather than a tautology.
		MaxMsgsPer: 400_000,
		Duplicates: 10 * time.Second,
		Storage:    nats.MemoryStorage,
		// R3 by default, because that is what the firehose runs. STRESS_STREAM_
		// REPLICAS=1 measures the same shape without RAFT quorum on the commit
		// path, which is the only way to price what replication costs rather
		// than guess at it.
		Replicas: env.GetInt("STRESS_STREAM_REPLICAS", 3),
		// Mandatory, not optional. NATS_PUBLISH_WIRE defaults to "fast" and the
		// fast wire needs allow_batched (API level 4); allow_atomic rides the same
		// flag so a run pinned to NATS_PUBLISH_WIRE=atomic works without a second
		// provision path. A stream created without them fails every multi-message
		// cohort at its first message.
		BatchPublish: true,
	}
}

// provisionStream converges the shadow stream and keeps converging it for the
// life of ctx, the same self-healing contract every stream owner gets. It is
// synchronous and fatal on the first pass: a rig that starts publishing into a
// stream that does not exist measures the broker's rejection path.
func provisionStream(ctx context.Context, url string, log *zap.Logger) error {
	return bus.EnsureStreams(ctx, url, []bus.StreamSpec{stressStream()}, log)
}

// teardownStream deletes the shadow stream. pkg/bus has no delete path by
// design — runtime credentials must not be able to erase the stream they own —
// so the rig issues it directly under the one credential the ACL grants
// STREAM.DELETE on this name, and only when a run explicitly asks for it.
func teardownStream(ctx context.Context) error {
	nc, err := controlConn("nats-stress-teardown")
	if err != nil {
		return err
	}
	defer nc.Close()

	js, err := jsapi.NewWithDomain(nc, bus.JSDomain())
	if err != nil {
		return fmt.Errorf("nats-stress: jetstream context: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, streamOpTimeout)
	defer cancel()
	if err := js.DeleteStream(ctx, stressStreamName); err != nil &&
		!errors.Is(err, jsapi.ErrStreamNotFound) {
		return fmt.Errorf("nats-stress: delete stream %q: %w", stressStreamName, err)
	}
	return nil
}

// controlConn dials the hub for the rig's own observation and teardown traffic.
//
// It is built here rather than borrowed from pkg/bus on purpose: pkg/bus exports
// no raw hub dialer, and the two things this connection does — poll STREAM.INFO
// once a second and delete the stream at the end — are operator traffic, not a
// measured path. Nothing that produces a number goes through it, so duplicating
// the endpoint/credential/TLS resolution here costs nothing in fidelity. It reads
// the same variables pkg/bus does so one env block configures both.
func controlConn(name string) (*nats.Conn, error) {
	url := env.Get("NATS_HUB_URL", env.Get("NATS_URL", ""))
	if url == "" {
		return nil, errors.New("nats-stress: NATS_HUB_URL (or NATS_URL) must be set")
	}
	opts := []nats.Option{
		nats.Name(name),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.Timeout(15 * time.Second),
		nats.DontRandomize(),
	}
	if user := env.Get("NATS_USER", ""); user != "" {
		opts = append(opts, nats.UserInfo(user, env.Get("NATS_PASSWORD", "")))
	}
	if secure := controlTLS(); secure != nil {
		opts = append(opts, secure)
	}
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats-stress: connect %q: %w", name, err)
	}
	return nc, nil
}

// controlTLS mirrors pkg/bus's posture: verify the broker against the fleet CA
// supplied inline as NATS_CA_PEM, and stay plaintext when none is configured so
// local development against an open server still works. SSL_CERT_FILE is
// deliberately not consulted — pkg/bus does not consult it either, and a rig
// whose control plane trusted a CA its measured connections did not would
// succeed here and fail there.
func controlTLS() nats.Option {
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
