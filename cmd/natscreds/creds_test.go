// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
)

func TestParseRoleKind(t *testing.T) {
	tests := []struct {
		role   string
		want   roleKind
		wantOK bool
	}{
		{"outgress_bus", roleKind{stem: "outgress", plane: "BUS"}, true},
		{"twitch_ingress_rpc", roleKind{stem: "twitch_ingress", plane: "RPC"}, true},
		{"sys", roleKind{}, false},
	}
	for _, tt := range tests {
		kind, ok := parseRoleKind(tt.role)
		if ok != tt.wantOK || kind != tt.want {
			t.Errorf("parseRoleKind(%q) = (%+v, %v), want (%+v, %v)", tt.role, kind, ok, tt.want, tt.wantOK)
		}
	}
}

func assertEnvNames(t *testing.T, kind roleKind, wantOK bool, want envNames) {
	t.Helper()
	got, ok := kind.envNames()
	if ok != wantOK || got != want {
		t.Fatalf("kind=%+v got=(%+v,%v), want=(%+v,%v)", kind, got, ok, want, wantOK)
	}
}

func TestResolveEnvNamesPlainService(t *testing.T) {
	assertEnvNames(t, roleKind{stem: "outgress", plane: "BUS"}, true,
		envNames{project: "outgress", jwtKey: "NATS_JWT", seedKey: "NATS_NKEY_SEED"})
	assertEnvNames(t, roleKind{stem: "outgress", plane: "RPC"}, true,
		envNames{project: "outgress", jwtKey: "NATS_RPC_JWT", seedKey: "NATS_RPC_NKEY_SEED"})
}

func TestResolveEnvNamesSharedProject(t *testing.T) {
	assertEnvNames(t, roleKind{stem: "discord_engine", plane: "BUS"}, true,
		envNames{project: "discord-svc", jwtKey: "DISCORD_ENGINE_BUS_JWT", seedKey: "DISCORD_ENGINE_BUS_NKEY_SEED"})
}

func TestResolveEnvNamesUnknownStem(t *testing.T) {
	assertEnvNames(t, roleKind{stem: "ghost", plane: "BUS"}, false, envNames{})
}

func TestResolveRoleTargetsExcludesSys(t *testing.T) {
	acl := &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{
		"SYS": {Roles: map[string]natsacl.RoleSpec{"sys": {}}},
		"BUS": {Roles: map[string]natsacl.RoleSpec{"outgress_bus": {}}},
	}}
	targets, err := resolveRoleTargets(acl)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].role != "outgress_bus" {
		t.Fatalf("targets = %+v, want exactly outgress_bus", targets)
	}
}

func TestMintUserCredentialHasEmptyPermissionLimits(t *testing.T) {
	acl := &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{
		"BUS": {Roles: map[string]natsacl.RoleSpec{"outgress_bus": {
			Publish: &natsacl.PermissionSpec{Allow: []string{"a.>"}},
		}}},
	}}
	mat := testMaterialFor(t, acl)
	ref := roleRef{account: "BUS", role: "outgress_bus"}

	token, seed, err := mintUserCredential(mat, ref)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.DecodeUserClaims(token)
	if err != nil {
		t.Fatal(err)
	}
	if !claims.HasEmptyPermissions() {
		t.Fatalf("UserPermissionLimits = %+v, want the zero value", claims.UserPermissionLimits)
	}
	assertIssuedByRole(t, mat, ref, claims)
	assertSeedSignsForSubject(t, seed, claims.Subject)
}

func assertIssuedByRole(t *testing.T, mat *material, ref roleRef, claims *jwt.UserClaims) {
	t.Helper()
	accountPub, err := mat.accountPub(ref.account)
	if err != nil {
		t.Fatal(err)
	}
	if claims.IssuerAccount != accountPub {
		t.Fatalf("IssuerAccount = %s, want %s", claims.IssuerAccount, accountPub)
	}
	rolePub, err := mat.roles[ref.account][ref.role].PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != rolePub {
		t.Fatalf("Issuer = %s, want the role signing key %s", claims.Issuer, rolePub)
	}
}

func TestRotateScopeMatches(t *testing.T) {
	var nilScope *rotateScope
	if nilScope.matches("outgress_bus") {
		t.Fatal("nil scope must never rotate")
	}
	all := parseRotateScope("all")
	if !all.matches("outgress_bus") || !all.matches("sys") {
		t.Fatal(`"all" must match every role`)
	}
	one := parseRotateScope("outgress_bus")
	if !one.matches("outgress_bus") || one.matches("commands_bus") {
		t.Fatal("a named scope must match only that role")
	}
}

func TestMintRoleCredentialSkipsWhenPresentAndNotRotating(t *testing.T) {
	acl := &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{
		"BUS": {Roles: map[string]natsacl.RoleSpec{"outgress_bus": {}}},
	}}
	mat := testMaterialFor(t, acl)
	store := newFakeDoppler()
	target := roleTarget{
		roleRef:  roleRef{account: "BUS", role: "outgress_bus"},
		envNames: envNames{project: "outgress", jwtKey: "NATS_JWT", seedKey: "NATS_NKEY_SEED"},
	}

	created, err := mintRoleCredential(store, mat, target, nil)
	mustMint(t, created, err)
	before := store.secrets["outgress"]["NATS_JWT"]

	created, err = mintRoleCredential(store, mat, target, nil)
	mustSkip(t, created, err)
	if store.secrets["outgress"]["NATS_JWT"] != before {
		t.Fatal("credential changed without a rotate request")
	}

	created, err = mintRoleCredential(store, mat, target, parseRotateScope("outgress_bus"))
	mustMint(t, created, err)
	if store.secrets["outgress"]["NATS_JWT"] == before {
		t.Fatal("rotate did not change the credential")
	}
}
