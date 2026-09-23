// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/rpc/deploy"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// deploySurface is the deployer's verb and event subjects, derived from the
// wire contract so a renamed prefix fails here instead of dropping silently.
var deploySurface = map[string]string{
	"service": deploy.Prefix + ".>",
	"stream":  deploy.EventsPrefix + ".>",
}

// TestDeployerSurfaceIsPrivateToAdmin pins the broker layer of the owner-only
// deploy gate. The deploy verbs start a production rollout, so both exports
// name ADMIN_RPC as their only importer, and ADMIN_RPC is the only account
// importing them. TestRPCRequestsAreImportedAndExported already refuses an
// import the export does not allow; this test refuses a quiet widening of the
// allowlist itself.
func TestDeployerSurfaceIsPrivateToAdmin(t *testing.T) {
	catalog := loadRPCCatalog(t)
	private := make(map[string][]string)
	for _, exp := range catalog.accounts["DEPLOYER_RPC"].exports {
		if exp.accounts != nil {
			private[exp.kind+" "+exp.subject] = exp.accounts
		}
	}
	want := make(map[string][]string, len(deploySurface))
	for kind, subject := range deploySurface {
		want[kind+" "+subject] = []string{"ADMIN_RPC"}
	}
	if !maps.EqualFunc(private, want, slices.Equal[[]string]) {
		t.Fatalf("DEPLOYER_RPC private exports differ:\nwant %v\n got %v", want, private)
	}
}

// TestOnlyAdminImportsDeploySurface lists every import of a deploy subject
// across all RPC accounts. Health is excluded: it is public by convention.
func TestOnlyAdminImportsDeploySurface(t *testing.T) {
	var got []string
	for name, account := range loadRPCCatalog(t).accounts {
		got = append(got, deployImports(name, account)...)
	}
	slices.Sort(got)
	want := []string{
		"ADMIN_RPC service " + deploySurface["service"],
		"ADMIN_RPC stream " + deploySurface["stream"],
	}
	if !slices.Equal(got, want) {
		t.Fatalf("deploy surface importers differ:\nwant %v\n got %v", want, got)
	}
}

func deployImports(name string, account rpcAccount) []string {
	var out []string
	for _, imp := range account.imports {
		if imp.account == "DEPLOYER_RPC" && imp.subject == deploySurface[imp.kind] {
			out = append(out, name+" "+imp.kind+" "+imp.subject)
		}
	}
	return out
}

// assertDeployerPlane runs the deployer's own NATS calls against the smoke
// broker: the run store on BUS, and the event fan-out to the console. The
// verb and owner-check hops run in assertNodeLocalRPCImport.
func (h *acceptanceHarness) assertDeployerPlane(t *testing.T, ctx context.Context) {
	t.Helper()
	h.assertDeployRunStore(t, ctx)
	h.assertDeployEventsReachAdminOnly(t)
	h.assertDeployVerbsRefuseDashboard(t)
}

// assertDeployRunStore walks every KV call app/deployer/internal/store makes
// (reconcile, get, create, compare-and-set update, revision-guarded delete).
// The bucket is R1 memory here because the smoke broker is one node; the API
// subjects are the ones bus.CoordinationBucket's R3 file bucket uses.
func (h *acceptanceHarness) assertDeployRunStore(t *testing.T, ctx context.Context) {
	t.Helper()
	nc, permissionErr := h.connectWithPermissionErrors(t, serviceIdentity{"deployer_bus"})
	defer nc.Close()
	js, err := jetstream.NewWithDomain(nc, "hub")
	if err != nil {
		t.Fatal(err)
	}
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: deploy.KVBucket, History: 1, Storage: jetstream.MemoryStorage, Replicas: 1,
	})
	if err != nil {
		t.Fatalf("deployer_bus could not reconcile %s: %v", deploy.KVBucket, err)
	}
	if err := runStoreRoundTrip(ctx, kv); err != nil {
		t.Fatalf("deployer_bus run store round trip: %v", err)
	}
	assertNoPermissionViolation(t, permissionErr, deploy.KVBucket)
}

func runStoreRoundTrip(ctx context.Context, kv jetstream.KeyValue) error {
	const key = "authz.run"
	rev, err := kv.Create(ctx, key, []byte("created"))
	if err != nil {
		return err
	}
	if _, err := kv.Get(ctx, key); err != nil {
		return err
	}
	rev, err = kv.Update(ctx, key, []byte("updated"), rev)
	if err != nil {
		return err
	}
	return kv.Delete(ctx, key, jetstream.LastRevision(rev))
}

// assertDeployEventsReachAdminOnly publishes one snapshot the way
// app/deployer/internal/events does and requires the console's account to see
// it and the dashboard's not to.
func (h *acceptanceHarness) assertDeployEventsReachAdminOnly(t *testing.T) {
	t.Helper()
	admin, adminMsgs := h.subscribeDeployEvents(t, serviceIdentity{"admin_rpc"})
	defer admin.Close()
	dashboard, dashboardMsgs := h.subscribeDeployEvents(t, serviceIdentity{"dashboard_rpc"})
	defer dashboard.Close()

	publisher, _ := h.connectWithPermissionErrors(t, serviceIdentity{"deployer_rpc"})
	defer publisher.Close()
	if err := publisher.Publish(deploy.EventsSubject("authz"), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := adminMsgs.NextMsg(time.Second); err != nil {
		t.Fatalf("admin_rpc did not receive the deploy event: %v", err)
	}
	if msg, err := dashboardMsgs.NextMsg(100 * time.Millisecond); err == nil {
		t.Fatalf("dashboard_rpc received a deploy event on %s", msg.Subject)
	}
}

func (h *acceptanceHarness) subscribeDeployEvents(t *testing.T, identity serviceIdentity) (*nats.Conn, *nats.Subscription) {
	t.Helper()
	nc, _ := h.connectWithPermissionErrors(t, identity)
	sub, err := nc.SubscribeSync(deploySurface["stream"])
	if err != nil {
		t.Fatal(err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	return nc, sub
}

// assertDeployVerbsRefuseDashboard answers every deploy verb and requires the
// dashboard's account, which imports none of them, to get no reply.
func (h *acceptanceHarness) assertDeployVerbsRefuseDashboard(t *testing.T) {
	t.Helper()
	responder, _ := h.connectWithPermissionErrors(t, serviceIdentity{"deployer_rpc"})
	defer responder.Close()
	if _, err := responder.QueueSubscribe(deploySurface["service"], "authz-deploy", func(msg *nats.Msg) {
		_ = msg.Respond([]byte("reached"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := responder.Flush(); err != nil {
		t.Fatal(err)
	}

	requester, _ := h.connectWithPermissionErrors(t, serviceIdentity{"dashboard_rpc"})
	defer requester.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := requester.RequestWithContext(ctx, deploy.Subject(deploy.VerbStart), nil); err == nil {
		t.Fatal("dashboard_rpc reached a deploy verb")
	}
}
