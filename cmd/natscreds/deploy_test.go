// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
)

func deploySysTestACL() *natsacl.ACL {
	return &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{
		"SYS": {Roles: map[string]natsacl.RoleSpec{"sys": {}}},
	}}
}

func TestMintDeployIdentityPermissionsAndSigningSeed(t *testing.T) {
	acl := deploySysTestACL()
	mat := testMaterialFor(t, acl)
	store := newFakeDoppler()

	created, err := mintDeployIdentity(store, mat, nil)
	mustMint(t, created, err)

	signingSeed, err := mat.operatorSigning.Seed()
	if err != nil {
		t.Fatal(err)
	}
	if store.secrets[deployerProject][deploySigningSeedKey] != string(signingSeed) {
		t.Fatal("DEPLOY_NATS_SIGNING_SEED does not match the operator signing seed")
	}

	claims, err := jwt.DecodeUserClaims(store.secrets[deployerProject][deploySysJWTKey])
	if err != nil {
		t.Fatal(err)
	}
	assertDeployPermissions(t, claims)
	assertIssuedBySYS(t, mat, claims)
}

func assertDeployPermissions(t *testing.T, claims *jwt.UserClaims) {
	t.Helper()
	wantPub := []string{"$SYS.REQ.CLAIMS.UPDATE", "$SYS.REQ.ACCOUNT.*.CLAIMS.LOOKUP", "$SYS.REQ.SERVER.PING"}
	if !equalStrings(claims.Pub.Allow, wantPub) {
		t.Fatalf("Pub.Allow = %v, want %v", claims.Pub.Allow, wantPub)
	}
	if !equalStrings(claims.Sub.Allow, []string{"_INBOX.>"}) {
		t.Fatalf("Sub.Allow = %v, want [_INBOX.>]", claims.Sub.Allow)
	}
}

func assertIssuedBySYS(t *testing.T, mat *material, claims *jwt.UserClaims) {
	t.Helper()
	accountPub, err := mat.accountPub(deploySysAccount)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != accountPub {
		t.Fatalf("Issuer = %s, want the SYS account key %s", claims.Issuer, accountPub)
	}
}

func TestMintDeployIdentitySkipsWithoutRotate(t *testing.T) {
	acl := deploySysTestACL()
	mat := testMaterialFor(t, acl)
	store := newFakeDoppler()

	created, err := mintDeployIdentity(store, mat, nil)
	mustMint(t, created, err)
	before := store.secrets[deployerProject][deploySysJWTKey]

	created, err = mintDeployIdentity(store, mat, nil)
	mustSkip(t, created, err)
	if store.secrets[deployerProject][deploySysJWTKey] != before {
		t.Fatal("deploy identity changed without a rotate request")
	}

	created, err = mintDeployIdentity(store, mat, parseRotateScope("all"))
	mustMint(t, created, err)
	if store.secrets[deployerProject][deploySysJWTKey] == before {
		t.Fatal(`"all" rotate did not change the deploy identity`)
	}
}
