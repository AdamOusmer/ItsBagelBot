// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"path/filepath"
	"testing"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

// writeOperatorJWTFixture mints and writes an operator.jwt for a test to
// build on, returning the raw token.
func writeOperatorJWTFixture(t *testing.T, mat *material, sysPub, path string) string {
	t.Helper()
	token, err := mintOperatorJWT(mat, sysPub)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeOperatorJWT(path, token); err != nil {
		t.Fatal(err)
	}
	return token
}

func randomAccountPub(t *testing.T) string {
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

func TestResolveOperatorJWTReusesMatchingFile(t *testing.T) {
	acl := deploySysTestACL()
	mat := testMaterialFor(t, acl)
	sysPub, err := mat.accountPub("SYS")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "operator.jwt")
	token := writeOperatorJWTFixture(t, mat, sysPub, path)

	reused, err := resolveOperatorJWT(path, mat, sysPub)
	if err != nil {
		t.Fatal(err)
	}
	if reused != token {
		t.Fatal("resolveOperatorJWT did not reuse the existing matching token verbatim")
	}
}

func TestResolveOperatorJWTMintsFreshWhenSystemAccountChanges(t *testing.T) {
	acl := deploySysTestACL()
	mat := testMaterialFor(t, acl)
	sysPub, err := mat.accountPub("SYS")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "operator.jwt")
	token := writeOperatorJWTFixture(t, mat, sysPub, path)
	otherPub := randomAccountPub(t)

	reissued, err := resolveOperatorJWT(path, mat, otherPub)
	if err != nil {
		t.Fatal(err)
	}
	if reissued == token {
		t.Fatal("expected a fresh operator jwt once the system account no longer matches")
	}
	claims, err := jwt.DecodeOperatorClaims(reissued)
	if err != nil {
		t.Fatal(err)
	}
	if claims.SystemAccount != otherPub {
		t.Fatalf("SystemAccount = %s, want %s", claims.SystemAccount, otherPub)
	}
}

func TestResolveOperatorJWTMintsFreshWhenFileMissing(t *testing.T) {
	acl := deploySysTestACL()
	mat := testMaterialFor(t, acl)
	sysPub, err := mat.accountPub("SYS")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "operator.jwt")

	token, err := resolveOperatorJWT(path, mat, sysPub)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected a freshly minted token when no file exists yet")
	}
}
