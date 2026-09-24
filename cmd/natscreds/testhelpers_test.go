// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"slices"
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/nkeys"
)

// testMaterialFor mints every seed a small ACL fixture needs, in memory,
// with no Doppler involved.
func testMaterialFor(t *testing.T, acl *natsacl.ACL) *material {
	t.Helper()
	mat := newMaterial(acl)
	for _, task := range buildKeyTasks(acl, mat) {
		kp, err := task.create()
		if err != nil {
			t.Fatal(err)
		}
		task.assign(kp)
	}
	return mat
}

func mustMint(t *testing.T, created bool, err error) {
	t.Helper()
	if err != nil || !created {
		t.Fatalf("created=%v err=%v, want a new credential", created, err)
	}
}

func mustSkip(t *testing.T, created bool, err error) {
	t.Helper()
	if err != nil || created {
		t.Fatalf("created=%v err=%v, want no-op", created, err)
	}
}

func assertContains(t *testing.T, lines []string, want string) {
	t.Helper()
	if !slices.Contains(lines, want) {
		t.Fatalf("lines = %v, want to contain %q", lines, want)
	}
}

func equalStrings(a, b []string) bool {
	return slices.Equal(a, b)
}

// assertSeedSignsForSubject proves a minted seed is the private half of a
// JWT's own subject: sign a nonce with the seed, verify with the subject's
// public key.
func assertSeedSignsForSubject(t *testing.T, seed, subject string) {
	t.Helper()
	userKP, err := nkeys.FromSeed([]byte(seed))
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("natscreds-test-nonce")
	sig, err := userKP.Sign(nonce)
	if err != nil {
		t.Fatal(err)
	}
	subjectKP, err := nkeys.FromPublicKey(subject)
	if err != nil {
		t.Fatal(err)
	}
	if err := subjectKP.Verify(nonce, sig); err != nil {
		t.Fatalf("seed's signature does not verify against the JWT subject: %v", err)
	}
}
