// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nats-io/jwt/v2"
)

func compileFixture(t *testing.T) ([]*jwt.AccountClaims, *ACL, *Keys) {
	t.Helper()
	acl := fixtureACL()
	keys := fixtureKeys(t, acl)
	claims, err := Compile(acl, keys)
	if err != nil {
		t.Fatal(err)
	}
	return claims, acl, keys
}

func byName(claims []*jwt.AccountClaims, name string) *jwt.AccountClaims {
	for _, c := range claims {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestCompileExports(t *testing.T) {
	claims, _, _ := compileFixture(t)
	exporter := byName(claims, "EXPORTER")
	if exporter == nil {
		t.Fatal("no EXPORTER claims")
	}
	if len(exporter.Exports) != 3 {
		t.Fatalf("got %d exports, want 3", len(exporter.Exports))
	}
	var sawStream, sawPrivate bool
	for _, e := range exporter.Exports {
		if e.Type == jwt.Stream {
			sawStream = true
		}
		if e.TokenReq {
			sawPrivate = true
		}
	}
	if !sawStream {
		t.Error("no stream export compiled")
	}
	if !sawPrivate {
		t.Error("the accounts-gated export did not set TokenReq")
	}
}

func TestCompileImportsPointAtExporterKey(t *testing.T) {
	claims, _, keys := compileFixture(t)
	importer := byName(claims, "IMPORTER")
	if importer == nil {
		t.Fatal("no IMPORTER claims")
	}
	if len(importer.Imports) != 2 {
		t.Fatalf("got %d imports, want 2", len(importer.Imports))
	}
	for _, imp := range importer.Imports {
		if imp.Account != keys.Accounts["EXPORTER"] {
			t.Errorf("import %q account = %s, want EXPORTER's key", imp.Subject, imp.Account)
		}
	}
}

func TestCompileJetStreamAndMappings(t *testing.T) {
	claims, _, _ := compileFixture(t)
	bus := byName(claims, "BUS")
	if bus.ClusterTraffic != jwt.ClusterTrafficOwner {
		t.Fatalf("ClusterTraffic = %q", bus.ClusterTraffic)
	}
	if !bus.Limits.JetStreamLimits.IsUnlimited() {
		t.Fatalf("JetStreamLimits = %+v, want unlimited", bus.Limits.JetStreamLimits)
	}
	// Literal, not hubDomainMappings() itself: a bug in that table must fail here too.
	want := jwt.Mapping{}
	for _, suffix := range []string{"INFO", "STREAM.>", "CONSUMER.>", "DIRECT.>", "META.>", "SERVER.>", "ACCOUNT.>", "$KV.>", "$OBJ.>"} {
		from := jwt.Subject("$JS.hub.API." + suffix)
		to := jwt.Subject("$JS.API." + suffix)
		if suffix == "$KV.>" || suffix == "$OBJ.>" {
			to = jwt.Subject(suffix)
		}
		want[from] = []jwt.WeightedMapping{{Subject: to}}
	}
	if !reflect.DeepEqual(bus.Mappings, want) {
		t.Fatalf("Mappings = %+v, want %+v", bus.Mappings, want)
	}

	exporter := byName(claims, "EXPORTER")
	if exporter.Limits.JetStreamLimits.MemoryStorage != 0 {
		t.Fatalf("EXPORTER should not have JetStream enabled: %+v", exporter.Limits.JetStreamLimits)
	}
}

func TestCompileRoleTemplateCarriesOnlyPermissions(t *testing.T) {
	claims, _, keys := compileFixture(t)
	bus := byName(claims, "BUS")
	scope, ok := bus.SigningKeys.GetScope(keys.Roles["BUS"]["worker_bus"])
	if !ok {
		t.Fatal("worker_bus signing key not found")
	}
	us, ok := scope.(*jwt.UserScope)
	if !ok {
		t.Fatalf("scope type = %T, want *jwt.UserScope", scope)
	}
	if us.Role != "worker_bus" {
		t.Fatalf("Role = %q", us.Role)
	}
	want := jwt.Permissions{
		Pub: jwt.Permission{Allow: []string{"work.>"}, Deny: []string{"work.secret.>"}},
		Sub: jwt.Permission{Allow: []string{"_INBOX.>"}},
	}
	if !reflect.DeepEqual(us.Template.Permissions, want) {
		t.Fatalf("Template.Permissions = %+v, want %+v", us.Template.Permissions, want)
	}
	if !us.Template.UserLimits.Empty() {
		t.Fatalf("scoped role template carries its own limits: %+v", us.Template.UserLimits)
	}
}

func TestCompileOrdersExportersBeforeImporters(t *testing.T) {
	claims, _, _ := compileFixture(t)
	positions := make(map[string]int, len(claims))
	for i, c := range claims {
		positions[c.Name] = i
	}
	if positions["EXPORTER"] > positions["IMPORTER"] {
		t.Fatalf("EXPORTER at %d, IMPORTER at %d: exporter must come first", positions["EXPORTER"], positions["IMPORTER"])
	}
}

func TestCompileOrdersCycleDeterministically(t *testing.T) {
	acl := &ACL{Accounts: map[string]AccountSpec{
		"ALFA": {Imports: []ImportSpec{{Service: "b.>", From: "BETA"}}},
		"BETA": {Imports: []ImportSpec{{Service: "a.>", From: "ALFA"}}},
	}}
	keys := fixtureKeys(t, acl)

	claims, err := Compile(acl, keys)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{claims[0].Name, claims[1].Name}
	want := []string{"ALFA", "BETA"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cycle order = %v, want %v (lexicographically first breaks the cycle)", got, want)
	}
}

func TestCompileValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		acl     *ACL
		mutate  func(keys *Keys)
		wantErr error
	}{
		{
			name: "unknown account import",
			acl: &ACL{Accounts: map[string]AccountSpec{
				"A": {Imports: []ImportSpec{{Service: "x.>", From: "GHOST"}}},
			}},
			wantErr: ErrUnknownAccount,
		},
		{
			name: "missing account key",
			acl: &ACL{Accounts: map[string]AccountSpec{
				"A": {},
			}},
			mutate:  func(keys *Keys) { delete(keys.Accounts, "A") },
			wantErr: ErrMissingAccountKey,
		},
		{
			name: "missing role key",
			acl: &ACL{Accounts: map[string]AccountSpec{
				"A": {Roles: map[string]RoleSpec{"role_a": {}}},
			}},
			mutate:  func(keys *Keys) { delete(keys.Roles["A"], "role_a") },
			wantErr: ErrMissingRoleKey,
		},
		{
			name: "duplicate role key",
			acl: &ACL{Accounts: map[string]AccountSpec{
				"A": {Roles: map[string]RoleSpec{"role_a": {}, "role_b": {}}},
			}},
			mutate: func(keys *Keys) {
				keys.Roles["A"]["role_b"] = keys.Roles["A"]["role_a"]
			},
			wantErr: ErrDuplicateRole,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := fixtureKeys(t, tt.acl)
			if tt.mutate != nil {
				tt.mutate(keys)
			}
			_, err := Compile(tt.acl, keys)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
