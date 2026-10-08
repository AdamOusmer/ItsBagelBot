// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl_test

import (
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

const validKeysYAML = `
operator: OABCDEF
accounts:
  BUS: AABCDEF
roles:
  BUS:
    outgress_bus: AZZZZZZ
`

func TestParseACL(t *testing.T) {
	acl, err := natsacl.ParseACL([]byte(validACLYAML))
	require.NoError(t, err)

	assert.Equal(t, "SYS", acl.SystemAccount)
	outgress := acl.Accounts["OUTGRESS_RPC"]
	assert.Equal(t, []natsacl.ExportSpec{{Service: "bagel.rpc.outgress.>"}}, outgress.Exports)
	assert.Equal(t, []natsacl.ImportSpec{{Service: "bagel.rpc.internal.tokens.>", From: "USERS_RPC"}}, outgress.Imports)
	bus := acl.Accounts["BUS"]
	assert.Equal(t, &natsacl.JetStreamSpec{ClusterTraffic: "owner"}, bus.JetStream)
	assert.Equal(t, "hub-domain", bus.Mappings)
	assert.Equal(t, &natsacl.PermissionSpec{Allow: []string{"a.>"}, Deny: []string{"a.secret.>"}}, bus.Roles["outgress_bus"].Publish)
}

func TestParseKeys(t *testing.T) {
	keys, err := natsacl.ParseKeys([]byte(validKeysYAML))
	require.NoError(t, err)

	assert.Equal(t, &natsacl.Keys{
		Operator: "OABCDEF",
		Accounts: map[string]string{"BUS": "AABCDEF"},
		Roles:    map[string]map[string]string{"BUS": {"outgress_bus": "AZZZZZZ"}},
	}, keys)
}

func TestParseRejectsUnknownFields(t *testing.T) {
	tests := []struct {
		name  string
		parse func() error
	}{
		{"ACL", func() error {
			_, err := natsacl.ParseACL([]byte("system_account: SYS\naccounts:\n  BUS:\n    unexpected_field: true\n"))
			return err
		}},
		{"Keys", func() error {
			_, err := natsacl.ParseKeys([]byte("operator: OABCDEF\naccounts: {}\nextra: nope\n"))
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Error(t, tt.parse())
		})
	}
}
