// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
	seed, err := opKp.Seed()
	require.NoError(t, err)
	return jwtFixture{opKp: opKp, opSeed: string(seed), exporterPub: pubKey(t), importerPub: pubKey(t)}
}

func pubKey(t *testing.T) string {
	t.Helper()
	kp, err := nkeys.CreateAccount()
	require.NoError(t, err)
	pub, err := kp.PublicKey()
	require.NoError(t, err)
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
	require.NoError(t, err)
	keys, err := natsacl.ParseKeys(f.keysYAML())
	require.NoError(t, err)
	claims, err := natsacl.Compile(acl, keys)
	require.NoError(t, err)
	return claims
}

func (f jwtFixture) goLive(t *testing.T, claims *fakeClaims, on []ports.ClusterName, accounts ...string) {
	t.Helper()
	for _, c := range f.compiled(t) {
		if len(accounts) > 0 && !slices.Contains(accounts, c.Name) {
			continue
		}
		tok, err := c.Encode(f.opKp)
		require.NoError(t, err)
		for _, cluster := range on {
			claims.live[cluster][c.Subject] = tok
		}
	}
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

var (
	bothClusters = []ports.ClusterName{ports.ClusterHub, ports.ClusterLeaf}
	hubOnly      = []ports.ClusterName{ports.ClusterHub}
)

func TestACLJWTRun(t *testing.T) {
	type outcome struct {
		Pushes []pushRecord
		Code   string
		Pushed bool
	}
	cases := []struct {
		name    string
		setup   func(*testing.T, jwtFixture, *fakeClaims)
		want    outcome
		errMsgs string
	}{
		{
			name:  "pushes only drifted accounts in order to both clusters",
			setup: func(*testing.T, jwtFixture, *fakeClaims) {},
			want: outcome{Pushed: true, Pushes: []pushRecord{
				{ports.ClusterHub, "EXPORTER"}, {ports.ClusterHub, "IMPORTER"},
				{ports.ClusterLeaf, "EXPORTER"}, {ports.ClusterLeaf, "IMPORTER"},
			}},
		},
		{
			name:  "skips accounts already live",
			setup: func(t *testing.T, f jwtFixture, c *fakeClaims) { f.goLive(t, c, bothClusters, "EXPORTER") },
			want:  outcome{Pushed: true, Pushes: []pushRecord{{ports.ClusterHub, "IMPORTER"}, {ports.ClusterLeaf, "IMPORTER"}}},
		},
		{
			name:  "records that nothing was pushed when every account is live",
			setup: func(t *testing.T, f jwtFixture, c *fakeClaims) { f.goLive(t, c, bothClusters) },
			want:  outcome{},
		},
		{
			name: "fails naming a server that never replied",
			setup: func(_ *testing.T, _ jwtFixture, c *fakeClaims) {
				c.servers[ports.ClusterHub] = []string{"s1", "s2"}
				c.replies[ports.ClusterHub] = []ports.ClaimsReply{{Server: "s1", Code: 200}}
			},
			want:    outcome{Code: string(deploy.FailClaimsPush), Pushes: []pushRecord{{ports.ClusterHub, "EXPORTER"}}},
			errMsgs: "s2",
		},
		{
			name: "fails naming a server that replied non-200",
			setup: func(_ *testing.T, _ jwtFixture, c *fakeClaims) {
				c.servers[ports.ClusterHub] = []string{"s1", "s2"}
				c.replies[ports.ClusterHub] = []ports.ClaimsReply{{Server: "s1", Code: 200}, {Server: "s2", Code: 500}}
			},
			want:    outcome{Code: string(deploy.FailClaimsPush), Pushes: []pushRecord{{ports.ClusterHub, "EXPORTER"}}},
			errMsgs: "s2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newJWTFixture(t)
			sink, claims, rc := jwtHarness(f)
			tc.setup(t, f, claims)

			err := (aclJWT{}).Run(context.Background(), rc)

			assert.Equal(t, tc.want, outcome{Pushes: claims.pushes, Code: errCode(err), Pushed: sink.View().Outputs.ACLPushed})
			if tc.errMsgs != "" {
				assert.ErrorContains(t, err, tc.errMsgs)
			}
		})
	}
}

func TestACLJWTDone(t *testing.T) {
	cases := []struct {
		name string
		live []ports.ClusterName
		want bool
	}{
		{"live matches git on both clusters", bothClusters, true},
		{"one cluster missing an account", hubOnly, false},
		{"nothing live anywhere", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newJWTFixture(t)
			_, claims, rc := jwtHarness(f)
			f.goLive(t, claims, tc.live)

			done, err := (aclJWT{}).Done(context.Background(), rc)

			require.NoError(t, err)
			assert.Equal(t, tc.want, done)
		})
	}
}
