package k8s

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

type acceptanceHarness struct {
	url      string
	password string
	log      *zap.Logger
}

type serviceIdentity struct {
	user string
}

type streamOwner struct {
	identity serviceIdentity
	specs    []bus.StreamSpec
}

type streamBinding struct {
	identity serviceIdentity
	group    string
	subject  string
}

type consumerGrant struct {
	identity serviceIdentity
	streams  []string
}

type publishProbe struct {
	identity serviceIdentity
	subject  string
}

type permissionProbe struct {
	identity serviceIdentity
	subject  string
}

type violationExpectation struct {
	permissionErr <-chan error
	subject       string
}

// TestScopedBusUsersBindAllowedStreams exercises the same pkg/bus stream and
// consumer calls the services make in production. It is opt-in because it
// needs the local NATS 2.14 fixture documented in NATS-ACCOUNTS.md.
func TestScopedBusUsersBindAllowedStreams(t *testing.T) {
	harness := newAcceptanceHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	harness.reconcileOwnedStreams(t, ctx)
	harness.assertAllowedBindings(t)
	harness.assertRequiredAckPermissions(t)
	harness.assertConsumerIsolation(t)
	harness.assertDestructiveOperationsDenied(t)
}

func newAcceptanceHarness(t *testing.T) *acceptanceHarness {
	t.Helper()
	url := os.Getenv("NATS_AUTHZ_ACCEPTANCE_URL")
	if url == "" {
		t.Skip("set NATS_AUTHZ_ACCEPTANCE_URL to run the local NATS authorization smoke test")
	}
	password := os.Getenv("NATS_AUTHZ_ACCEPTANCE_PASSWORD")
	if password == "" {
		t.Fatal("NATS_AUTHZ_ACCEPTANCE_PASSWORD is required")
	}
	harness := &acceptanceHarness{url: url, password: password, log: zap.NewNop()}
	harness.configureEnv(t)
	return harness
}

func (h *acceptanceHarness) reconcileOwnedStreams(t *testing.T, ctx context.Context) {
	t.Helper()
	owners := []streamOwner{
		{serviceIdentity{"users_bus"}, []bus.StreamSpec{localStream(bus.BagelDataStream)}},
		{serviceIdentity{"worker_bus"}, []bus.StreamSpec{localStream(bus.TwitchIngressStream)}},
		{serviceIdentity{"outgress_bus"}, []bus.StreamSpec{localStream(bus.OutgressStream), localStream(bus.OutgressSystemStream)}},
	}
	for _, owner := range owners {
		h.activate(t, owner.identity)
		if err := bus.EnsureStreams(ctx, h.url, owner.specs, h.log); err != nil {
			t.Fatalf("%s could not reconcile its owned stream(s): %v", owner.identity.user, err)
		}
	}
}

func (h *acceptanceHarness) assertAllowedBindings(t *testing.T) {
	t.Helper()
	bindings := []streamBinding{
		// Both broadcast (ephemeral) and durable paths are represented.
		{serviceIdentity{"users_bus"}, "", "data.users.changed"},
		{serviceIdentity{"users_bus"}, "authz_users", "data.reproject.request"},
		{serviceIdentity{"commands_bus"}, "", "data.commands.changed"},
		{serviceIdentity{"commands_bus"}, "authz_commands", "data.commands.used"},
		{serviceIdentity{"modules_bus"}, "", "data.modules.changed"},
		{serviceIdentity{"modules_bus"}, "authz_modules", "data.users.deleted"},
		{serviceIdentity{"loyalty_bus"}, "authz_loyalty", "data.loyalty.earned"},
		{serviceIdentity{"projector_bus"}, "authz_projector", "data.users.changed"},
		{serviceIdentity{"projector_bus"}, "authz_projector", "twitch.ingress.event.stream"},
		{serviceIdentity{"worker_bus"}, "authz_worker", "twitch.ingress.event.premium"},
		{serviceIdentity{"outgress_bus"}, "authz_outgress", "twitch.outgress.premium"},
		{serviceIdentity{"outgress_bus"}, "authz_outgress", "twitch.outgress.system"},
		{serviceIdentity{"outgress_bus"}, "authz_outgress", "twitch.ingress.event.stream"},
		// Authorization lifecycle consumers (revocation marking + grant re-enroll).
		{serviceIdentity{"outgress_bus"}, "authz_outgress", "twitch.ingress.status.authz.granted"},
		{serviceIdentity{"outgress_bus"}, "authz_outgress", "twitch.ingress.status.authz.revoked"},
		{serviceIdentity{"outgress_bus"}, "authz_outgress", "twitch.ingress.status.authz.subrevoked"},
	}
	for _, binding := range bindings {
		binding := binding
		t.Run("bind_"+binding.identity.user+"_"+binding.subject, func(t *testing.T) {
			h.assertAllowedBinding(t, binding)
		})
	}
}

func (h *acceptanceHarness) assertAllowedBinding(t *testing.T, binding streamBinding) {
	t.Helper()
	h.activate(t, binding.identity)
	sub, err := bus.NewSubscriber(h.url, binding.group, h.log)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Close() }()
	subCtx, stop := context.WithCancel(context.Background())
	defer stop()
	if _, err := sub.Subscribe(subCtx, binding.subject); err != nil {
		t.Fatalf("allowed binding failed: %v", err)
	}
}

func (h *acceptanceHarness) assertRequiredAckPermissions(t *testing.T) {
	t.Helper()
	grants := []consumerGrant{
		{serviceIdentity{"users_bus"}, []string{"BAGEL_DATA"}},
		{serviceIdentity{"commands_bus"}, []string{"BAGEL_DATA"}},
		{serviceIdentity{"modules_bus"}, []string{"BAGEL_DATA"}},
		{serviceIdentity{"loyalty_bus"}, []string{"BAGEL_DATA"}},
		{serviceIdentity{"projector_bus"}, []string{"BAGEL_DATA", "TWITCH_INGRESS"}},
		{serviceIdentity{"worker_bus"}, []string{"TWITCH_INGRESS"}},
		{serviceIdentity{"outgress_bus"}, []string{"TWITCH_OUTGRESS", "TWITCH_OUTGRESS_SYSTEM", "TWITCH_INGRESS"}},
	}
	for _, grant := range grants {
		h.assertStreamAcks(t, grant)
	}
}

func (h *acceptanceHarness) assertStreamAcks(t *testing.T, grant consumerGrant) {
	t.Helper()
	for _, stream := range grant.streams {
		// NATS 2.14 may issue either legacy or domain/account-qualified ACK
		// reply subjects. Exercise both permission shapes directly.
		h.assertPublishAllowed(t, publishProbe{grant.identity, "$JS.ACK." + stream + ".authz.1.1.1.0.0"})
		h.assertPublishAllowed(t, publishProbe{grant.identity, "$JS.ACK.hub.account." + stream + ".authz.1.1.1.0.0"})
	}
}

func (h *acceptanceHarness) assertPublishAllowed(t *testing.T, probe publishProbe) {
	t.Helper()
	nc, permissionErr := h.connectWithPermissionErrors(t, probe.identity)
	defer nc.Close()
	if err := nc.Publish(probe.subject, nil); err != nil {
		t.Fatal(err)
	}
	if err := nc.FlushTimeout(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-permissionErr:
		t.Fatalf("%s could not publish required ACK %s: %v", probe.identity.user, probe.subject, err)
	case <-time.After(25 * time.Millisecond):
	}
}

func (h *acceptanceHarness) assertConsumerIsolation(t *testing.T) {
	t.Helper()
	identity := serviceIdentity{"loyalty_bus"}
	nc, permissionErr := h.connectWithPermissionErrors(t, identity)
	defer nc.Close()
	js, err := nc.JetStream(nats.Domain("hub"), nats.MaxWait(300*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	// Leave Name/Durable empty so nats.go sends the legacy create endpoint with
	// the wildcard filter in the JSON body, not as an invalid publish subject.
	_, err = js.AddConsumer("TWITCH_INGRESS", &nats.ConsumerConfig{
		DeliverSubject: "_INBOX.authz.loyalty.stolen",
		DeliverPolicy:  nats.DeliverNewPolicy,
		AckPolicy:      nats.AckExplicitPolicy,
		FilterSubject:  "twitch.ingress.event.>",
	})
	if err == nil {
		t.Fatal("loyalty_bus unexpectedly created a TWITCH_INGRESS consumer")
	}
	assertAuthorizationViolation(t, violationExpectation{
		permissionErr: permissionErr,
		subject:       "$JS.API.CONSUMER.CREATE.TWITCH_INGRESS",
	})
}

func (h *acceptanceHarness) assertDestructiveOperationsDenied(t *testing.T) {
	t.Helper()
	identities := []serviceIdentity{
		{"users_bus"}, {"commands_bus"}, {"modules_bus"}, {"loyalty_bus"},
		{"transactions_bus"}, {"projector_bus"}, {"worker_bus"}, {"outgress_bus"},
		{"twitch_ingress_bus"}, {"dashboard_bus"},
	}
	for _, identity := range identities {
		h.assertDestructiveOperationsDeniedFor(t, identity)
	}
}

func (h *acceptanceHarness) assertDestructiveOperationsDeniedFor(t *testing.T, identity serviceIdentity) {
	t.Helper()
	for _, operation := range []string{"PURGE", "DELETE"} {
		operation := operation
		t.Run(fmt.Sprintf("deny_%s_%s", identity.user, operation), func(t *testing.T) {
			h.assertRequestDenied(t, permissionProbe{
				identity: identity,
				subject:  "$JS.API.STREAM." + operation + ".BAGEL_DATA",
			})
		})
	}
}

func localStream(spec bus.StreamSpec) bus.StreamSpec {
	spec.Storage = nats.MemoryStorage
	spec.Replicas = 1
	spec.PlacementTags = nil
	spec.MaxBytes = 8 << 20
	return spec
}

func (h *acceptanceHarness) configureEnv(t *testing.T) {
	t.Helper()
	t.Setenv("NATS_USER", "")
	t.Setenv("NATS_PASSWORD", "")
	t.Setenv("NATS_HUB_URL", h.url)
	t.Setenv("NATS_HUB_PUBLISH_URL", h.url)
	t.Setenv("NATS_JS_DOMAIN", "hub")
	t.Setenv("NATS_CA_PEM", "")
	t.Setenv("NATS_PUBLISH_CONNECTIONS", "1")
}

func (h *acceptanceHarness) activate(t *testing.T, identity serviceIdentity) {
	t.Helper()
	if err := os.Setenv("NATS_USER", identity.user); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("NATS_PASSWORD", h.password); err != nil {
		t.Fatal(err)
	}
}

func (h *acceptanceHarness) assertRequestDenied(t *testing.T, probe permissionProbe) {
	t.Helper()
	nc, permissionErr := h.connectWithPermissionErrors(t, probe.identity)
	defer nc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := nc.RequestWithContext(ctx, probe.subject, nil); err == nil {
		t.Fatalf("%s unexpectedly received a response to forbidden request %s", probe.identity.user, probe.subject)
	}
	assertAuthorizationViolation(t, violationExpectation{permissionErr: permissionErr, subject: probe.subject})
}

func (h *acceptanceHarness) connectWithPermissionErrors(t *testing.T, identity serviceIdentity) (*nats.Conn, <-chan error) {
	t.Helper()
	permissionErr := make(chan error, 1)
	nc, err := nats.Connect(h.url,
		nats.UserInfo(identity.user, h.password),
		nats.Timeout(300*time.Millisecond),
		nats.MaxReconnects(0),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			select {
			case permissionErr <- err:
			default:
			}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return nc, permissionErr
}

func assertAuthorizationViolation(t *testing.T, expectation violationExpectation) {
	t.Helper()
	select {
	case err := <-expectation.permissionErr:
		if !strings.Contains(err.Error(), "Permissions Violation") || !strings.Contains(err.Error(), expectation.subject) {
			t.Fatalf("denial was not the expected authorization violation for %s: %v", expectation.subject, err)
		}
	case <-time.After(time.Second):
		t.Fatalf("no authorization violation reported for forbidden subject %s", expectation.subject)
	}
}
