// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl

import (
	"testing"

	"github.com/nats-io/nkeys"
)

func newKey(t *testing.T) string {
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

// EXPORTER has a service, stream, and private export; IMPORTER imports both;
// BUS carries jetstream, hub-domain mappings, and two roles (one with deny).
func fixtureACL() *ACL {
	return &ACL{
		Accounts: map[string]AccountSpec{
			"EXPORTER": {
				Exports: []ExportSpec{
					{Service: "svc.public.>"},
					{Stream: "stream.public.>"},
					{Service: "svc.private.>", Accounts: []string{"IMPORTER"}},
				},
			},
			"IMPORTER": {
				Imports: []ImportSpec{
					{Service: "svc.public.>", From: "EXPORTER"},
					{Stream: "stream.public.>", From: "EXPORTER"},
				},
			},
			"BUS": {
				JetStream: &JetStreamSpec{ClusterTraffic: "owner"},
				Mappings:  "hub-domain",
				Roles: map[string]RoleSpec{
					"worker_bus": {
						Publish:   &PermissionSpec{Allow: []string{"work.>"}, Deny: []string{"work.secret.>"}},
						Subscribe: &PermissionSpec{Allow: []string{"_INBOX.>"}},
					},
					"admin_bus": {
						Publish: &PermissionSpec{Allow: []string{"admin.>"}},
					},
				},
			},
		},
	}
}

func fixtureKeys(t *testing.T, acl *ACL) *Keys {
	t.Helper()
	keys := &Keys{
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
