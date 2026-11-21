// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"slices"
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const controlsGatewayAccount = "CONTROLS_GATEWAY_RPC"

func TestTTSOverlayGrantIsolation(t *testing.T) {
	catalog := loadRPCCatalog(t)
	for _, subject := range []string{
		"rpc.internal.users.voice.overlay.resolve",
		"rpc.internal.users.voice.overlay.resolve.node.n1",
		"rpc.internal.users.voice.overlay.check",
		"rpc.internal.users.voice.overlay.check.node.n1",
		"controls.tts.revoked.42",
	} {
		assertOverlayGrant(t, catalog, subject)
	}
}

func assertOverlayGrant(t *testing.T, catalog rpcCatalog, subject string) {
	t.Helper()
	users := catalog.byUser["users_rpc"]
	match := func(candidate string) bool { return server.SubjectsCollide(candidate, subject) }
	index := slices.IndexFunc(users.exports, func(grant rpcGrant) bool { return match(grant.subject) })
	require.NotEqual(t, -1, index, "Users does not export %s", subject)
	export := users.exports[index]
	assert.Equal(t, []string{controlsGatewayAccount}, export.accounts)

	for _, account := range catalog.accounts {
		got := slices.IndexFunc(account.imports, func(imp rpcImport) bool { return match(imp.subject) })
		if account.name == controlsGatewayAccount {
			require.NotEqual(t, -1, got, "%s must import %s", account.name, subject)
			want := rpcImport{rpcGrant: rpcGrant{kind: export.kind, subject: export.subject}, account: users.name}
			assert.Equal(t, want, account.imports[got])
		} else {
			assert.Equal(t, -1, got, "%s imports %s", account.name, subject)
		}
	}
}

func TestTTSOverlayReaderPermissionsAreExact(t *testing.T) {
	account := committedACL(t).Accounts[controlsGatewayAccount]
	assert.NotNil(t, account.JetStream)
	assert.Empty(t, account.Exports)
	require.Len(t, account.Imports, 5)
	require.Contains(t, account.Roles, "controls_gateway_rpc")

	role := account.Roles["controls_gateway_rpc"]
	assert.Equal(t, natsacl.RoleSpec{
		Publish: &natsacl.PermissionSpec{Allow: []string{
			"rpc.internal.users.voice.overlay.resolve",
			"rpc.internal.users.voice.overlay.resolve.node.*",
			"rpc.internal.users.voice.overlay.check",
			"rpc.internal.users.voice.overlay.check.node.*",
			"$JS.API.CONSUMER.CREATE.OVERLAY_TTS.*.overlay.tts.*",
			"$JS.FC.OVERLAY_TTS.*.*",
			"$JS.FC.*.*.OVERLAY_TTS.*.*",
			"$JS.API.STREAM.MSG.GET.OBJ_TTS_CLIPS",
		}},
		Subscribe: &natsacl.PermissionSpec{Allow: []string{"controls.tts.revoked.*", "_INBOX.>"}},
	}, role)
}
