// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package aclpush_test

import (
	"context"
	"testing"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/clusterops/internal/aclpush"
)

type fakeCP struct {
	servers []string
	live    map[string]string
	replies []aclpush.ClaimsReply
	pushed  []string
}

func (f *fakeCP) Servers(context.Context, aclpush.ClusterName) ([]string, error) {
	return f.servers, nil
}

func (f *fakeCP) Lookup(_ context.Context, _ aclpush.ClusterName, key string) (string, error) {
	return f.live[key], nil
}

func (f *fakeCP) Push(_ context.Context, _ aclpush.ClusterName, accountJWT string, _ int) ([]aclpush.ClaimsReply, error) {
	f.pushed = append(f.pushed, accountJWT)
	return f.replies, nil
}

func namedClaims(t *testing.T, name string) *jwt.AccountClaims {
	t.Helper()
	kp, err := nkeys.CreateAccount()
	require.NoError(t, err)
	pub, err := kp.PublicKey()
	require.NoError(t, err)
	c := jwt.NewAccountClaims(pub)
	c.Name = name
	return c
}

func operator(t *testing.T) nkeys.KeyPair {
	t.Helper()
	op, err := nkeys.CreateOperator()
	require.NoError(t, err)
	return op
}

func encode(t *testing.T, c *jwt.AccountClaims, signer nkeys.KeyPair) string {
	t.Helper()
	token, err := c.Encode(signer)
	require.NoError(t, err)
	return token
}

func names(claims []*jwt.AccountClaims) []string {
	var out []string
	for _, c := range claims {
		out = append(out, c.Name)
	}
	return out
}

func TestDrift(t *testing.T) {
	op := operator(t)
	matching := namedClaims(t, "MATCHING")
	drifted := namedClaims(t, "DRIFTED")
	missing := namedClaims(t, "MISSING")
	live := map[string]string{
		matching.Subject: encode(t, matching, op),
		drifted.Subject:  encode(t, namedClaims(t, "OLD"), op),
	}

	cases := []struct {
		name     string
		live     map[string]string
		compiled []*jwt.AccountClaims
		want     []string
	}{
		{"pushes only what is missing or different", live, []*jwt.AccountClaims{matching, drifted, missing}, []string{"DRIFTED", "MISSING"}},
		{"counts an account with no live claims as different", map[string]string{}, []*jwt.AccountClaims{namedClaims(t, "SOLO")}, []string{"SOLO"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := aclpush.NewClusterPusher(context.Background(), &fakeCP{live: tc.live}, op, aclpush.ClusterHub)
			require.NoError(t, err)

			got, err := p.Drift(context.Background(), tc.compiled)
			require.NoError(t, err)
			assert.Equal(t, tc.want, names(got))
		})
	}
}

func TestPush(t *testing.T) {
	cases := []struct {
		name    string
		replies []aclpush.ClaimsReply
		want    *aclpush.PushError
	}{
		{"succeeds when every server replies 200", []aclpush.ClaimsReply{{Server: "s1", Code: 200}, {Server: "s2", Code: 200}}, nil},
		{"fails naming a server that never replies", []aclpush.ClaimsReply{{Server: "s1", Code: 200}}, &aclpush.PushError{Missing: []string{"s2"}}},
		{"fails naming a server that replies without code 200", []aclpush.ClaimsReply{
			{Server: "s1", Code: 200}, {Server: "s2", Code: 500, Message: "boom"},
		}, &aclpush.PushError{Bad: []string{"s2"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := &fakeCP{servers: []string{"s1", "s2"}, replies: tc.replies}
			p, err := aclpush.NewClusterPusher(context.Background(), cp, operator(t), aclpush.ClusterHub)
			require.NoError(t, err)

			res, err := p.Push(context.Background(), namedClaims(t, "PUSHED"))

			assert.Equal(t, aclpush.PushResult{Account: "PUSHED", Cluster: aclpush.ClusterHub, Replies: tc.replies}, res)
			assert.Len(t, cp.pushed, 1)
			if tc.want == nil {
				require.NoError(t, err)
				return
			}
			var pe *aclpush.PushError
			require.ErrorAs(t, err, &pe)
			assert.Equal(t, tc.want, pe)
		})
	}
}

func TestSync(t *testing.T) {
	op := operator(t)
	matching, drifted := namedClaims(t, "MATCHING"), namedClaims(t, "DRIFTED")
	ok := []aclpush.ClaimsReply{{Server: "s1", Code: 200}}
	cases := []struct {
		name    string
		replies []aclpush.ClaimsReply
		want    []aclpush.ClusterName
		wantErr bool
	}{
		{"pushes the drifted account to hub then leaf", ok, []aclpush.ClusterName{aclpush.ClusterHub, aclpush.ClusterLeaf}, false},
		{"stops at the hub when a server refuses", []aclpush.ClaimsReply{{Server: "s1", Code: 500}}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := &fakeCP{servers: []string{"s1"}, replies: tc.replies, live: map[string]string{matching.Subject: encode(t, matching, op)}}

			res, err := aclpush.Sync(context.Background(), cp, op, []*jwt.AccountClaims{matching, drifted})

			var clusters []aclpush.ClusterName
			for _, r := range res {
				assert.Equal(t, "DRIFTED", r.Account)
				clusters = append(clusters, r.Cluster)
			}
			assert.Equal(t, tc.want, clusters)
			assert.Equal(t, tc.wantErr, err != nil)
		})
	}
}

func TestSigner(t *testing.T) {
	op := operator(t)
	seed, err := op.Seed()
	require.NoError(t, err)

	signer, err := aclpush.Signer(string(seed))
	require.NoError(t, err)
	want, _ := op.PublicKey()
	got, _ := signer.PublicKey()
	assert.Equal(t, want, got, "valid seed round-trips to the same key")

	_, err = aclpush.Signer("not-a-seed")
	assert.Error(t, err, "garbage seed is rejected")
}
