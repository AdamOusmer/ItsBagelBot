// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const roleACL = `system_account: SYS
accounts:
  SYS:
    roles:
      sys: {}
  BUS:
    roles:
      outgress_bus:
        publish:
          allow: [a.>]
      outgress_rpc: {}
      discord_engine_bus: {}
      worker_bus: {}
      twitch_ingress_rpc: {}
`

const systemACL = "system_account: SYS\naccounts:\n  SYS: {}\n"

func accountRoles(account string, roles ...string) string {
	acl := "  " + account + ":\n    roles:\n"
	for _, role := range roles {
		acl += "      " + role + ": {}\n"
	}
	return acl
}

func serviceCredentialSecrets(h *harness) []string {
	var names []string
	for name := range snapshotSecrets(h.store) {
		if project, _, _ := strings.Cut(name, "/"); project != operatorProject && project != deployerProject {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func TestRoleCredentialsLandInTheirDopplerProject(t *testing.T) {
	h := newHarness(t, roleACL)

	h.apply(t)

	assert.Equal(t, []string{
		"discord-svc/DISCORD_ENGINE_BUS_JWT", "discord-svc/DISCORD_ENGINE_BUS_NKEY_SEED",
		"outgress/NATS_JWT", "outgress/NATS_NKEY_SEED",
		"outgress/NATS_RPC_JWT", "outgress/NATS_RPC_NKEY_SEED",
		"sesame/NATS_JWT", "sesame/NATS_NKEY_SEED",
		"twitch-ingress/NATS_RPC_JWT", "twitch-ingress/NATS_RPC_NKEY_SEED",
	}, serviceCredentialSecrets(h))
}

func TestRunsFailForRolesThatCannotMapToACredential(t *testing.T) {
	tests := []struct {
		name    string
		acl     string
		wantErr string
	}{
		{name: "rejects a role name used in two accounts", acl: systemACL + accountRoles("A", "outgress_bus") + accountRoles("B", "outgress_bus"), wantErr: ErrDuplicateRoleName.Error()},
		{name: "rejects a role whose service has no Doppler project", acl: systemACL + accountRoles("BUS", "ghost_bus"), wantErr: "no doppler project mapping"},
		{name: "rejects a bus role for a service that has no bus", acl: systemACL + accountRoles("BUS", "gossip_bus"), wantErr: "NO_BUS"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.acl)

			assert.ErrorContains(t, h.applyErr(), tc.wantErr)
		})
	}
}

func TestMintedUserCredentialIsIssuedByTheRoleKeyWithNoPermissionLimits(t *testing.T) {
	h := newHarness(t, roleACL)
	h.apply(t)
	keys := h.keys(t)

	claims, err := jwt.DecodeUserClaims(h.secret("outgress", "NATS_JWT"))

	require.NoError(t, err)
	assert.True(t, claims.HasEmptyPermissions(), "permissions live in the account JWT, not the user JWT")
	assert.Equal(t, keys.Accounts["BUS"], claims.IssuerAccount)
	assert.Equal(t, keys.Roles["BUS"]["outgress_bus"], claims.Issuer)
	seed, err := nkeys.FromSeed([]byte(h.secret("outgress", "NATS_NKEY_SEED")))
	require.NoError(t, err)
	subject, err := nkeys.FromPublicKey(claims.Subject)
	require.NoError(t, err)
	nonce := []byte("natscreds-test-nonce")
	signature, err := seed.Sign(nonce)
	require.NoError(t, err)
	assert.NoError(t, subject.Verify(nonce, signature), "seed must be the private half of the JWT subject")
}

func TestDeployIdentityCanOnlyManageClaimsAndItsSigningSeedIsTheOperators(t *testing.T) {
	h := newHarness(t, roleACL)
	h.apply(t)

	claims, err := jwt.DecodeUserClaims(h.secret(deployerProject, deploySysJWTKey))

	require.NoError(t, err)
	assert.Equal(t, []string{"$SYS.REQ.CLAIMS.UPDATE", "$SYS.REQ.ACCOUNT.*.CLAIMS.LOOKUP", "$SYS.REQ.SERVER.PING"}, []string(claims.Pub.Allow))
	assert.Equal(t, []string{"_INBOX.>"}, []string(claims.Sub.Allow))
	assert.Equal(t, h.keys(t).Accounts["SYS"], claims.Issuer)
	assert.Equal(t, h.secret(operatorProject, "NATS_OPERATOR_SIGNING_SEED"), h.secret(deployerProject, deploySigningSeedKey))
}
