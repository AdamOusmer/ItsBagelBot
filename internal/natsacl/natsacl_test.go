// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl_test

import (
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/require"
)

func newKey(t *testing.T) string {
	t.Helper()
	kp, err := nkeys.CreateAccount()
	require.NoError(t, err)
	pub, err := kp.PublicKey()
	require.NoError(t, err)
	return pub
}

// EXPORTER has a service, stream, and private export; IMPORTER imports both;
// BUS carries jetstream, hub-domain mappings, and two roles (one with deny).
func fixtureACL() *natsacl.ACL {
	return &natsacl.ACL{
		Accounts: map[string]natsacl.AccountSpec{
			"EXPORTER": {
				Exports: []natsacl.ExportSpec{
					{Service: "svc.public.>"},
					{Stream: "stream.public.>"},
					{Service: "svc.private.>", Accounts: []string{"IMPORTER"}},
				},
			},
			"IMPORTER": {
				Imports: []natsacl.ImportSpec{
					{Service: "svc.public.>", From: "EXPORTER"},
					{Stream: "stream.public.>", From: "EXPORTER"},
				},
			},
			"BUS": {
				JetStream: &natsacl.JetStreamSpec{ClusterTraffic: "owner"},
				Mappings:  "hub-domain",
				Roles: map[string]natsacl.RoleSpec{
					"worker_bus": {
						Publish:   &natsacl.PermissionSpec{Allow: []string{"work.>"}, Deny: []string{"work.secret.>"}},
						Subscribe: &natsacl.PermissionSpec{Allow: []string{"_INBOX.>"}},
					},
					"admin_bus": {Publish: &natsacl.PermissionSpec{Allow: []string{"admin.>"}}},
				},
			},
		},
	}
}

func fixtureKeys(t *testing.T, acl *natsacl.ACL) *natsacl.Keys {
	t.Helper()
	keys := &natsacl.Keys{
		Operator: newKey(t),
		Accounts: make(map[string]string, len(acl.Accounts)),
		Roles:    make(map[string]map[string]string, len(acl.Accounts)),
	}
	for name, spec := range acl.Accounts {
		keys.Accounts[name] = newKey(t)
		roles := make(map[string]string, len(spec.Roles))
		for role := range spec.Roles {
			roles[role] = newKey(t)
		}
		keys.Roles[name] = roles
	}
	return keys
}

func compileFixture(t *testing.T) ([]*jwt.AccountClaims, *natsacl.Keys) {
	t.Helper()
	acl := fixtureACL()
	keys := fixtureKeys(t, acl)
	claims, err := natsacl.Compile(acl, keys)
	require.NoError(t, err)
	return claims, keys
}

func byName(t *testing.T, claims []*jwt.AccountClaims, name string) *jwt.AccountClaims {
	t.Helper()
	for _, c := range claims {
		if c.Name == name {
			return c
		}
	}
	require.FailNow(t, "no claims compiled", name)
	return nil
}
