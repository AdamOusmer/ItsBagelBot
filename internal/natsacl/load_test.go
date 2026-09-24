// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl

import (
	"reflect"
	"testing"
)

const validACLYAML = `
system_account: SYS
accounts:
  OUTGRESS_RPC:
    exports:
      - service: bagel.rpc.outgress.>
    imports:
      - {service: bagel.rpc.internal.tokens.>, from: USERS_RPC}
    roles:
      outgress_rpc: {}
  USERS_RPC:
    roles:
      users_rpc: {}
  BUS:
    jetstream: {cluster_traffic: owner}
    mappings: hub-domain
    roles:
      outgress_bus:
        publish: {allow: ["a.>"], deny: ["a.secret.>"]}
        subscribe: {allow: ["_INBOX.>"]}
  SYS:
    roles:
      sys: {}
`

func parseFixtureACL(t *testing.T) *ACL {
	t.Helper()
	acl, err := ParseACL([]byte(validACLYAML))
	if err != nil {
		t.Fatal(err)
	}
	return acl
}

func TestParseACLSystemAccount(t *testing.T) {
	acl := parseFixtureACL(t)
	if acl.SystemAccount != "SYS" {
		t.Fatalf("system_account = %q, want SYS", acl.SystemAccount)
	}
}

func TestParseACLExportsAndImports(t *testing.T) {
	outgress := parseFixtureACL(t).Accounts["OUTGRESS_RPC"]
	wantExports := []ExportSpec{{Service: "bagel.rpc.outgress.>"}}
	if !reflect.DeepEqual(outgress.Exports, wantExports) {
		t.Fatalf("OUTGRESS_RPC exports = %+v, want %+v", outgress.Exports, wantExports)
	}
	wantImports := []ImportSpec{{Service: "bagel.rpc.internal.tokens.>", From: "USERS_RPC"}}
	if !reflect.DeepEqual(outgress.Imports, wantImports) {
		t.Fatalf("OUTGRESS_RPC imports = %+v, want %+v", outgress.Imports, wantImports)
	}
}

func TestParseACLJetStreamAndMappings(t *testing.T) {
	bus := parseFixtureACL(t).Accounts["BUS"]
	wantJetStream := &JetStreamSpec{ClusterTraffic: "owner"}
	if !reflect.DeepEqual(bus.JetStream, wantJetStream) {
		t.Fatalf("BUS jetstream = %+v, want %+v", bus.JetStream, wantJetStream)
	}
	if bus.Mappings != "hub-domain" {
		t.Fatalf("BUS mappings = %q", bus.Mappings)
	}
}

func TestParseACLRolePermissions(t *testing.T) {
	role := parseFixtureACL(t).Accounts["BUS"].Roles["outgress_bus"]
	want := &PermissionSpec{Allow: []string{"a.>"}, Deny: []string{"a.secret.>"}}
	if !reflect.DeepEqual(role.Publish, want) {
		t.Fatalf("outgress_bus publish = %+v, want %+v", role.Publish, want)
	}
}

func TestParseKeysRoundTrip(t *testing.T) {
	const doc = `
operator: OABCDEF
accounts:
  BUS: AABCDEF
roles:
  BUS:
    outgress_bus: AZZZZZZ
`
	keys, err := ParseKeys([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if keys.Operator != "OABCDEF" {
		t.Fatalf("operator = %q", keys.Operator)
	}
	if keys.Accounts["BUS"] != "AABCDEF" {
		t.Fatalf("accounts[BUS] = %q", keys.Accounts["BUS"])
	}
	if keys.Roles["BUS"]["outgress_bus"] != "AZZZZZZ" {
		t.Fatalf("roles[BUS][outgress_bus] = %q", keys.Roles["BUS"]["outgress_bus"])
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	const badACL = `
system_account: SYS
accounts:
  BUS:
    unexpected_field: true
`
	const badKeys = `
operator: OABCDEF
accounts: {}
extra: nope
`
	tests := []struct {
		name  string
		parse func() error
	}{
		{"ACL", func() error { _, err := ParseACL([]byte(badACL)); return err }},
		{"Keys", func() error { _, err := ParseKeys([]byte(badKeys)); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.parse() == nil {
				t.Fatal("expected an error for an unknown field")
			}
		})
	}
}
