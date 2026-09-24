// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package reconcile

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const fixtureACLYAML = `accounts:
  EXPORTER:
    exports:
      - service: svc.>
  IMPORTER:
    imports:
      - {service: svc.>, from: EXPORTER}
`

type fixture struct {
	opKp        nkeys.KeyPair
	opSeed      string
	exporterPub string
	importerPub string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	opKp, err := nkeys.CreateOperator()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := opKp.Seed()
	if err != nil {
		t.Fatal(err)
	}
	return fixture{opKp: opKp, opSeed: string(seed), exporterPub: newAccountPub(t), importerPub: newAccountPub(t)}
}

func newAccountPub(t *testing.T) string {
	t.Helper()
	kp, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := kp.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func (f fixture) opPub() string {
	pub, _ := f.opKp.PublicKey()
	return pub
}

func (f fixture) keysYAML() []byte {
	return []byte(fmt.Sprintf("operator: %s\naccounts:\n  EXPORTER: %s\n  IMPORTER: %s\n",
		f.opPub(), f.exporterPub, f.importerPub))
}

func testCfg(seed string) ports.Config {
	return ports.Config{
		AccountsFile:      "deploy/messaging/accounts.yaml",
		AccountsKeysFile:  "deploy/messaging/accounts.keys.yaml",
		NATSSigningSeed:   seed,
		ACLReconcileEvery: time.Millisecond,
	}
}

type fakeGitHub struct {
	ports.GitHub
	acl, keys []byte
	gotRef    ports.Ref
	calls     int
}

func (g *fakeGitHub) File(_ context.Context, path ports.FilePath, ref ports.Ref) ([]byte, error) {
	g.calls++
	g.gotRef = ref
	switch path {
	case "deploy/messaging/accounts.yaml":
		return g.acl, nil
	case "deploy/messaging/accounts.keys.yaml":
		return g.keys, nil
	}
	return nil, fmt.Errorf("unexpected file %s", path)
}

type fakeClaims struct {
	servers map[ports.ClusterName][]string
	live    map[ports.ClusterName]map[string]string
	replies map[ports.ClusterName][]ports.ClaimsReply
	pushes  []string
}

func (f *fakeClaims) Servers(_ context.Context, cluster ports.ClusterName) ([]string, error) {
	return f.servers[cluster], nil
}

func (f *fakeClaims) Lookup(_ context.Context, cluster ports.ClusterName, key string) (string, error) {
	return f.live[cluster][key], nil
}

func (f *fakeClaims) Push(_ context.Context, cluster ports.ClusterName, accountJWT string, _ int) ([]ports.ClaimsReply, error) {
	c, err := jwt.DecodeAccountClaims(accountJWT)
	if err != nil {
		return nil, err
	}
	f.pushes = append(f.pushes, string(cluster)+"/"+c.Name)
	return f.replies[cluster], nil
}

type fakeStore struct {
	ports.Store
	locked bool
	runs   []deploy.RunSummary
	byID   map[deploy.RunID]deploy.Run
}

func (s *fakeStore) LockOwner(context.Context) (deploy.RunID, bool, error) { return "", s.locked, nil }

func (s *fakeStore) List(context.Context, int) ([]deploy.RunSummary, error) { return s.runs, nil }

func (s *fakeStore) Get(_ context.Context, id deploy.RunID) (deploy.Run, ports.Revision, error) {
	run, ok := s.byID[id]
	if !ok {
		return deploy.Run{}, 0, fmt.Errorf("run %s not found", id)
	}
	return run, 1, nil
}

func drivenClaims() *fakeClaims {
	return &fakeClaims{
		servers: map[ports.ClusterName][]string{ports.ClusterHub: {"s1"}, ports.ClusterLeaf: {"s1"}},
		live:    map[ports.ClusterName]map[string]string{ports.ClusterHub: {}, ports.ClusterLeaf: {}},
		replies: map[ports.ClusterName][]ports.ClaimsReply{
			ports.ClusterHub:  {{Server: "s1", Code: 200}},
			ports.ClusterLeaf: {{Server: "s1", Code: 200}},
		},
	}
}

func TestTickSkipsWhileLocked(t *testing.T) {
	f := newFixture(t)
	gh := &fakeGitHub{acl: []byte(fixtureACLYAML), keys: f.keysYAML()}
	claims := drivenClaims()
	store := &fakeStore{
		locked: true,
		// A real successful run to reconcile against: if the lock check were
		// skipped, tick would still find work and this test would not catch it.
		runs: []deploy.RunSummary{{ID: "r1", State: deploy.RunSucceeded}},
		byID: map[deploy.RunID]deploy.Run{"r1": {Outputs: deploy.Outputs{PinSHA: "pinsha1"}}},
	}
	d := Deps{Store: store, GitHub: gh, Claims: claims, Config: testCfg(f.opSeed), Log: zap.NewNop()}

	if err := tick(context.Background(), d); err != nil {
		t.Fatalf("tick() error = %v", err)
	}
	if gh.calls != 0 {
		t.Fatalf("github reads = %d, want 0: reconcile must not run while the cluster lock is held", gh.calls)
	}
	if len(claims.pushes) != 0 {
		t.Fatalf("pushes = %v, want none while locked", claims.pushes)
	}
}

func TestTickRepairsDriftAgainstLastSuccessfulRunsPin(t *testing.T) {
	f := newFixture(t)
	gh := &fakeGitHub{acl: []byte(fixtureACLYAML), keys: f.keysYAML()}
	claims := drivenClaims()
	store := &fakeStore{
		runs: []deploy.RunSummary{
			{ID: "r2", State: deploy.RunFailed},
			{ID: "r1", State: deploy.RunSucceeded},
		},
		byID: map[deploy.RunID]deploy.Run{
			"r1": {Outputs: deploy.Outputs{PinSHA: "pinsha1"}},
			"r2": {Outputs: deploy.Outputs{PinSHA: "pinsha2"}},
		},
	}
	core, logs := observer.New(zapcore.InfoLevel)
	d := Deps{Store: store, GitHub: gh, Claims: claims, Config: testCfg(f.opSeed), Log: zap.New(core)}

	if err := tick(context.Background(), d); err != nil {
		t.Fatalf("tick() error = %v", err)
	}
	if gh.gotRef != "pinsha1" {
		t.Fatalf("ref read = %q, want the last succeeded run's pin pinsha1", gh.gotRef)
	}
	if len(claims.pushes) == 0 {
		t.Fatal("expected the drifted accounts to be pushed")
	}
	if logs.FilterMessage("acl reconcile pushed drifted account claims").Len() == 0 {
		t.Fatal("expected a log line naming what reconcile pushed")
	}
}

func TestTickSkipsWhenNoRunEverSucceeded(t *testing.T) {
	f := newFixture(t)
	gh := &fakeGitHub{acl: []byte(fixtureACLYAML), keys: f.keysYAML()}
	claims := drivenClaims()
	store := &fakeStore{runs: []deploy.RunSummary{{ID: "r1", State: deploy.RunFailed}}}
	d := Deps{Store: store, GitHub: gh, Claims: claims, Config: testCfg(f.opSeed), Log: zap.NewNop()}

	if err := tick(context.Background(), d); err != nil {
		t.Fatalf("tick() error = %v", err)
	}
	if gh.calls != 0 || len(claims.pushes) != 0 {
		t.Fatalf("github calls = %d, pushes = %v; want neither with no successful run", gh.calls, claims.pushes)
	}
}
