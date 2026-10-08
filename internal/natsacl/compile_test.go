// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl_test

import (
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileFixtureAccounts(t *testing.T) {
	claims, keys := compileFixture(t)

	t.Run("exports carry a stream and an accounts-gated token requirement", func(t *testing.T) {
		type grant struct {
			Type     jwt.ExportType
			TokenReq bool
		}
		var got []grant
		for _, e := range byName(t, claims, "EXPORTER").Exports {
			got = append(got, grant{e.Type, e.TokenReq})
		}
		assert.ElementsMatch(t, []grant{{jwt.Service, false}, {jwt.Stream, false}, {jwt.Service, true}}, got)
	})

	t.Run("imports point at the exporter's account key", func(t *testing.T) {
		imports := byName(t, claims, "IMPORTER").Imports
		require.Len(t, imports, 2)
		for _, imp := range imports {
			assert.Equal(t, keys.Accounts["EXPORTER"], imp.Account, string(imp.Subject))
		}
	})

	t.Run("bus gets unlimited jetstream and the hub domain mappings", func(t *testing.T) {
		bus := byName(t, claims, "BUS")
		assert.Equal(t, jwt.ClusterTraffic(jwt.ClusterTrafficOwner), bus.ClusterTraffic)
		assert.True(t, bus.Limits.JetStreamLimits.IsUnlimited())
		want := jwt.Mapping{}
		for _, suffix := range []string{"INFO", "STREAM.>", "CONSUMER.>", "DIRECT.>", "META.>", "SERVER.>", "ACCOUNT.>"} {
			want[jwt.Subject("$JS.hub.API."+suffix)] = []jwt.WeightedMapping{{Subject: jwt.Subject("$JS.API." + suffix)}}
		}
		for _, suffix := range []string{"$KV.>", "$OBJ.>"} {
			want[jwt.Subject("$JS.hub.API."+suffix)] = []jwt.WeightedMapping{{Subject: jwt.Subject(suffix)}}
		}
		assert.Equal(t, want, bus.Mappings)
		assert.Zero(t, byName(t, claims, "EXPORTER").Limits.JetStreamLimits.MemoryStorage, "only accounts that ask for jetstream get it")
	})

	t.Run("role templates carry permissions and no limits", func(t *testing.T) {
		scope, ok := byName(t, claims, "BUS").SigningKeys.GetScope(keys.Roles["BUS"]["worker_bus"])
		require.True(t, ok)
		us, ok := scope.(*jwt.UserScope)
		require.True(t, ok)
		assert.Equal(t, "worker_bus", us.Role)
		assert.Equal(t, jwt.Permissions{
			Pub: jwt.Permission{Allow: []string{"work.>"}, Deny: []string{"work.secret.>"}},
			Sub: jwt.Permission{Allow: []string{"_INBOX.>"}},
		}, us.Template.Permissions)
		assert.True(t, us.Template.UserLimits.Empty())
	})

	t.Run("exporters compile before importers", func(t *testing.T) {
		positions := map[string]int{}
		for i, c := range claims {
			positions[c.Name] = i
		}
		assert.Less(t, positions["EXPORTER"], positions["IMPORTER"])
	})
}

func TestCompileOrdersAnImportCycleDeterministically(t *testing.T) {
	acl := &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{
		"ALFA": {Imports: []natsacl.ImportSpec{{Service: "b.>", From: "BETA"}}},
		"BETA": {Imports: []natsacl.ImportSpec{{Service: "a.>", From: "ALFA"}}},
	}}

	claims, err := natsacl.Compile(acl, fixtureKeys(t, acl))

	require.NoError(t, err)
	assert.Equal(t, []string{"ALFA", "BETA"}, []string{claims[0].Name, claims[1].Name})
}

func TestCompileTokenRequiredImports(t *testing.T) {
	gated := &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{
		"EXPORTER": {Exports: []natsacl.ExportSpec{{Service: "svc.gated.>", Accounts: []string{"IMPORTER"}}}},
		"IMPORTER": {Imports: []natsacl.ImportSpec{{Service: "svc.gated.>", From: "EXPORTER"}}},
	}}
	activation := map[string][]natsacl.Activation{"IMPORTER": {{From: "EXPORTER", Subject: "svc.gated.>", Token: "test-token"}}}

	t.Run("attaches the activation token to the import", func(t *testing.T) {
		keys := fixtureKeys(t, gated)
		keys.Activations = activation
		claims, err := natsacl.Compile(gated, keys)
		require.NoError(t, err)
		imports := byName(t, claims, "IMPORTER").Imports
		require.Len(t, imports, 1)
		assert.Equal(t, "test-token", imports[0].Token)
	})

	t.Run("fails when the activation is missing", func(t *testing.T) {
		_, err := natsacl.Compile(gated, fixtureKeys(t, gated))
		assert.ErrorIs(t, err, natsacl.ErrMissingActivation)
	})
}

func TestCompileRejectsInconsistentInput(t *testing.T) {
	roles := map[string]natsacl.AccountSpec{"A": {Roles: map[string]natsacl.RoleSpec{"role_a": {}, "role_b": {}}}}
	tests := []struct {
		name    string
		acl     *natsacl.ACL
		mutate  func(keys *natsacl.Keys)
		wantErr error
	}{
		{name: "rejects an import from an unknown account", acl: &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{"A": {Imports: []natsacl.ImportSpec{{Service: "x.>", From: "GHOST"}}}}}, wantErr: natsacl.ErrUnknownAccount},
		{name: "rejects an account with no key", acl: &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{"A": {}}}, mutate: func(keys *natsacl.Keys) { delete(keys.Accounts, "A") }, wantErr: natsacl.ErrMissingAccountKey},
		{name: "rejects a role with no key", acl: &natsacl.ACL{Accounts: roles}, mutate: func(keys *natsacl.Keys) { delete(keys.Roles["A"], "role_a") }, wantErr: natsacl.ErrMissingRoleKey},
		{name: "rejects two roles sharing a key", acl: &natsacl.ACL{Accounts: roles}, mutate: func(keys *natsacl.Keys) { keys.Roles["A"]["role_b"] = keys.Roles["A"]["role_a"] }, wantErr: natsacl.ErrDuplicateRole},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := fixtureKeys(t, tt.acl)
			if tt.mutate != nil {
				tt.mutate(keys)
			}
			_, err := natsacl.Compile(tt.acl, keys)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestEquivalent(t *testing.T) {
	claims, _ := compileFixture(t)
	exporter := byName(t, claims, "EXPORTER")
	resigned := *exporter
	resigned.ID, resigned.IssuedAt, resigned.Issuer = "different-jti", 1234, "OACCOUNT"
	reordered := *exporter
	reordered.Exports = jwt.Exports{exporter.Exports[2], exporter.Exports[0], exporter.Exports[1]}
	dropped := *exporter
	dropped.Exports = append(jwt.Exports{}, exporter.Exports[:len(exporter.Exports)-1]...)
	tests := []struct {
		name string
		a, b *jwt.AccountClaims
		want bool
	}{
		{name: "ignores signing metadata", a: exporter, b: &resigned, want: true},
		{name: "ignores export order", a: exporter, b: &reordered, want: true},
		{name: "detects a dropped grant", a: exporter, b: &dropped, want: false},
		{name: "never equates a claim with nil", a: exporter, b: nil, want: false},
		{name: "equates nil with nil", a: nil, b: nil, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, natsacl.Equivalent(tt.a, tt.b))
		})
	}
}
