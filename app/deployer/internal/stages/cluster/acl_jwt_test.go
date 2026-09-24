// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	"go.uber.org/zap"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/internal/natsacl"
)

const (
	testAccountsFile     = ports.FilePath("deploy/messaging/accounts.yaml")
	testAccountsKeysFile = ports.FilePath("deploy/messaging/accounts.keys.yaml")
)

const jwtACLYAML = `accounts:
  EXPORTER:
    exports:
      - service: svc.>
  IMPORTER:
    imports:
      - {service: svc.>, from: EXPORTER}
`

type jwtFixture struct {
	opKp        nkeys.KeyPair
	opSeed      string
	exporterPub string
	importerPub string
}

func newJWTFixture(t *testing.T) jwtFixture {
	t.Helper()
	opKp, err := nkeys.CreateOperator()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := opKp.Seed()
	if err != nil {
		t.Fatal(err)
	}
	return jwtFixture{opKp: opKp, opSeed: string(seed), exporterPub: pubKey(t), importerPub: pubKey(t)}
}

func pubKey(t *testing.T) string {
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

func (f jwtFixture) opPub() string {
	pub, _ := f.opKp.PublicKey()
	return pub
}

func (f jwtFixture) keysYAML() []byte {
	return []byte(fmt.Sprintf("operator: %s\naccounts:\n  EXPORTER: %s\n  IMPORTER: %s\n",
		f.opPub(), f.exporterPub, f.importerPub))
}

func (f jwtFixture) compiled(t *testing.T) []*jwt.AccountClaims {
	t.Helper()
	acl, err := natsacl.ParseACL([]byte(jwtACLYAML))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := natsacl.ParseKeys(f.keysYAML())
	if err != nil {
		t.Fatal(err)
	}
	claims, err := natsacl.Compile(acl, keys)
	if err != nil {
		t.Fatal(err)
	}
	return claims
}

func (f jwtFixture) byName(t *testing.T, name string) *jwt.AccountClaims {
	t.Helper()
	for _, c := range f.compiled(t) {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no compiled claims named %s", name)
	return nil
}

func (f jwtFixture) sign(t *testing.T, c *jwt.AccountClaims) string {
	t.Helper()
	tok, err := c.Encode(f.opKp)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func testJWTConfig(seed string) ports.Config {
	cfg := testConfig()
	cfg.AccountsFile, cfg.AccountsKeysFile, cfg.NATSSigningSeed = testAccountsFile, testAccountsKeysFile, seed
	return cfg
}

type jwtGitHub struct {
	*fakeGitHub
	acl, keys []byte
}

func (g jwtGitHub) File(_ context.Context, path ports.FilePath, _ ports.Ref) ([]byte, error) {
	switch path {
	case testAccountsFile:
		return g.acl, nil
	case testAccountsKeysFile:
		return g.keys, nil
	}
	return nil, fmt.Errorf("unexpected file %s", path)
}

type pushRecord struct {
	cluster ports.ClusterName
	account string
}

type fakeClaims struct {
	servers map[ports.ClusterName][]string
	live    map[ports.ClusterName]map[string]string
	replies map[ports.ClusterName][]ports.ClaimsReply
	pushes  []pushRecord
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
	f.pushes = append(f.pushes, pushRecord{cluster: cluster, account: c.Name})
	return f.replies[cluster], nil
}

func jwtHarness(f jwtFixture) (*fakeSink, *fakeClaims, *stage.RunCtx) {
	sink := newSink(deploy.KindReapply)
	gh := jwtGitHub{fakeGitHub: &fakeGitHub{}, acl: []byte(jwtACLYAML), keys: f.keysYAML()}
	claims := &fakeClaims{
		servers: map[ports.ClusterName][]string{ports.ClusterHub: {"s1"}, ports.ClusterLeaf: {"s1"}},
		live:    map[ports.ClusterName]map[string]string{ports.ClusterHub: {}, ports.ClusterLeaf: {}},
		replies: map[ports.ClusterName][]ports.ClaimsReply{
			ports.ClusterHub:  {{Server: "s1", Code: 200}},
			ports.ClusterLeaf: {{Server: "s1", Code: 200}},
		},
	}
	deps := stage.Deps{
		GitHub: gh, Applier: &fakeApplier{builds: map[ports.FilePath]ports.Objects{}}, Watcher: &fakeWatcher{},
		Claims: claims, Clock: fixedClock{}, Config: testJWTConfig(f.opSeed), Log: zap.NewNop(),
	}
	return sink, claims, stage.New(deploy.StageACL, deps, sink)
}

func TestACLJWTPushesOnlyDriftedAccountsInOrderToBothClusters(t *testing.T) {
	f := newJWTFixture(t)
	_, claims, rc := jwtHarness(f)

	if err := (aclJWT{}).Run(context.Background(), rc); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []pushRecord{
		{ports.ClusterHub, "EXPORTER"}, {ports.ClusterHub, "IMPORTER"},
		{ports.ClusterLeaf, "EXPORTER"}, {ports.ClusterLeaf, "IMPORTER"},
	}
	if !reflect.DeepEqual(claims.pushes, want) {
		t.Fatalf("pushes = %+v, want %+v", claims.pushes, want)
	}
}

func TestACLJWTSkipsAccountsAlreadyLive(t *testing.T) {
	f := newJWTFixture(t)
	_, claims, rc := jwtHarness(f)
	exporter := f.byName(t, "EXPORTER")
	live := f.sign(t, exporter)
	claims.live[ports.ClusterHub][f.exporterPub] = live
	claims.live[ports.ClusterLeaf][f.exporterPub] = live

	if err := (aclJWT{}).Run(context.Background(), rc); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []pushRecord{{ports.ClusterHub, "IMPORTER"}, {ports.ClusterLeaf, "IMPORTER"}}
	if !reflect.DeepEqual(claims.pushes, want) {
		t.Fatalf("pushes = %+v, want %+v (EXPORTER already matches live)", claims.pushes, want)
	}
}

func TestACLJWTRecordsWhetherAnythingWasPushed(t *testing.T) {
	f := newJWTFixture(t)
	sink, claims, rc := jwtHarness(f)
	for _, c := range f.compiled(t) {
		live := f.sign(t, c)
		claims.live[ports.ClusterHub][c.Subject] = live
		claims.live[ports.ClusterLeaf][c.Subject] = live
	}

	if err := (aclJWT{}).Run(context.Background(), rc); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if sink.View().Outputs.ACLPushed {
		t.Fatal("ACLPushed = true, want false: nothing was drifted")
	}
}

func TestACLJWTFailsNamingAMissingReply(t *testing.T) {
	f := newJWTFixture(t)
	_, claims, rc := jwtHarness(f)
	claims.servers[ports.ClusterHub] = []string{"s1", "s2"}
	claims.replies[ports.ClusterHub] = []ports.ClaimsReply{{Server: "s1", Code: 200}}

	err := (aclJWT{}).Run(context.Background(), rc)
	if err == nil || !strings.Contains(err.Error(), "s2") {
		t.Fatalf("err = %v, want it to name s2", err)
	}
	f2, ok := ports.AsFail(err)
	if !ok || f2.Code != deploy.FailClaimsPush {
		t.Fatalf("failure code = %+v, want %s", f2, deploy.FailClaimsPush)
	}
}

func TestACLJWTFailsNamingANon200Reply(t *testing.T) {
	f := newJWTFixture(t)
	_, claims, rc := jwtHarness(f)
	claims.servers[ports.ClusterHub] = []string{"s1", "s2"}
	claims.replies[ports.ClusterHub] = []ports.ClaimsReply{{Server: "s1", Code: 200}, {Server: "s2", Code: 500}}

	err := (aclJWT{}).Run(context.Background(), rc)
	if err == nil || !strings.Contains(err.Error(), "s2") {
		t.Fatalf("err = %v, want it to name s2", err)
	}
}

func TestACLJWTDone(t *testing.T) {
	f := newJWTFixture(t)
	cases := []struct {
		name string
		seed func(claims *fakeClaims)
		want bool
	}{
		{
			name: "live matches git on both clusters",
			seed: func(claims *fakeClaims) {
				for _, c := range f.compiled(t) {
					live := f.sign(t, c)
					claims.live[ports.ClusterHub][c.Subject] = live
					claims.live[ports.ClusterLeaf][c.Subject] = live
				}
			},
			want: true,
		},
		{
			name: "one cluster missing an account",
			seed: func(claims *fakeClaims) {
				for _, c := range f.compiled(t) {
					claims.live[ports.ClusterHub][c.Subject] = f.sign(t, c)
				}
			},
			want: false,
		},
		{name: "nothing live anywhere", seed: func(*fakeClaims) {}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, claims, rc := jwtHarness(f)
			tc.seed(claims)
			done, err := (aclJWT{}).Done(context.Background(), rc)
			if err != nil || done != tc.want {
				t.Fatalf("Done() = %v, %v; want %v", done, err, tc.want)
			}
		})
	}
}
