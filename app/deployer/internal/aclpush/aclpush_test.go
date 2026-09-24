// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package aclpush

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"

	"ItsBagelBot/app/deployer/internal/ports"
)

type fakeCP struct {
	servers []string
	live    map[string]string
	replies []ports.ClaimsReply
	pushErr error
	pushed  []string
}

func (f *fakeCP) Servers(context.Context, ports.ClusterName) ([]string, error) { return f.servers, nil }

func (f *fakeCP) Lookup(_ context.Context, _ ports.ClusterName, key string) (string, error) {
	return f.live[key], nil
}

func (f *fakeCP) Push(_ context.Context, _ ports.ClusterName, accountJWT string, _ int) ([]ports.ClaimsReply, error) {
	f.pushed = append(f.pushed, accountJWT)
	return f.replies, f.pushErr
}

func newKeyPair(t *testing.T) nkeys.KeyPair {
	t.Helper()
	kp, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	return kp
}

func namedClaims(t *testing.T, name string) *jwt.AccountClaims {
	t.Helper()
	kp := newKeyPair(t)
	pub, err := kp.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	c := jwt.NewAccountClaims(pub)
	c.Name = name
	return c
}

func encode(t *testing.T, c *jwt.AccountClaims, signer nkeys.KeyPair) string {
	t.Helper()
	token, err := c.Encode(signer)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestDriftPushesOnlyWhatIsMissingOrDifferent(t *testing.T) {
	op, err := nkeys.CreateOperator()
	if err != nil {
		t.Fatal(err)
	}
	matching := namedClaims(t, "MATCHING")
	drifted := namedClaims(t, "DRIFTED")
	missing := namedClaims(t, "MISSING")

	fake := &fakeCP{live: map[string]string{
		matching.Subject: encode(t, matching, op),
		drifted.Subject:  encode(t, namedClaims(t, "OLD"), op),
	}}
	p := &ClusterPusher{cp: fake, cluster: ports.ClusterHub, signer: op}

	got, err := p.Drift(context.Background(), []*jwt.AccountClaims{matching, drifted, missing})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range got {
		names = append(names, c.Name)
	}
	want := []string{"DRIFTED", "MISSING"}
	if !slices.Equal(names, want) {
		t.Fatalf("Drift() = %v, want %v", names, want)
	}
}

func TestDrift_MissingCountsAsDifferent(t *testing.T) {
	op, _ := nkeys.CreateOperator()
	c := namedClaims(t, "SOLO")
	fake := &fakeCP{live: map[string]string{}}
	p := &ClusterPusher{cp: fake, cluster: ports.ClusterLeaf, signer: op}

	got, err := p.Drift(context.Background(), []*jwt.AccountClaims{c})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Drift() = %v, want the one account with no live claims", got)
	}
}

func TestPushSucceedsWhenEveryServerReplies200(t *testing.T) {
	op, _ := nkeys.CreateOperator()
	fake := &fakeCP{servers: []string{"s1", "s2"}, replies: []ports.ClaimsReply{
		{Server: "s1", Code: 200}, {Server: "s2", Code: 200},
	}}
	p := &ClusterPusher{cp: fake, cluster: ports.ClusterHub, signer: op, servers: fake.servers}

	c := namedClaims(t, "PUSHED")
	res, err := p.Push(context.Background(), c)
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if res.Account != "PUSHED" || res.Cluster != ports.ClusterHub {
		t.Fatalf("Push() result = %+v", res)
	}
	if len(fake.pushed) != 1 {
		t.Fatalf("push count = %d, want 1", len(fake.pushed))
	}
}

func TestPushFailsNamingTheBadServer(t *testing.T) {
	cases := []struct {
		name        string
		replies     []ports.ClaimsReply
		wantMissing []string
		wantBad     []string
	}{
		{"a server never replies", []ports.ClaimsReply{{Server: "s1", Code: 200}}, []string{"s2"}, nil},
		{"a server replies without code 200", []ports.ClaimsReply{
			{Server: "s1", Code: 200}, {Server: "s2", Code: 500, Message: "boom"},
		}, nil, []string{"s2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op, _ := nkeys.CreateOperator()
			fake := &fakeCP{servers: []string{"s1", "s2"}, replies: tc.replies}
			p := &ClusterPusher{cp: fake, cluster: ports.ClusterHub, signer: op, servers: fake.servers}

			_, err := p.Push(context.Background(), namedClaims(t, "X"))
			var pe *PushError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want *PushError", err)
			}
			if !slices.Equal(pe.Missing, tc.wantMissing) || !slices.Equal(pe.Bad, tc.wantBad) {
				t.Fatalf("PushError = %+v, want Missing=%v Bad=%v", pe, tc.wantMissing, tc.wantBad)
			}
		})
	}
}

func TestSignerRoundTrips(t *testing.T) {
	kp, err := nkeys.CreateOperator()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := kp.Seed()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := Signer(string(seed))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := kp.PublicKey()
	got, _ := signer.PublicKey()
	if got != want {
		t.Fatalf("Signer public key = %s, want %s", got, want)
	}
}

func TestSignerRejectsGarbage(t *testing.T) {
	if _, err := Signer("not-a-seed"); err == nil {
		t.Fatal("expected an error for a garbage seed")
	}
}
