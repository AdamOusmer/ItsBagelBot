// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"encoding/base64"
	"slices"
	"testing"
	"time"

	"ItsBagelBot/internal/natsacl"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const controlsGatewayAccount = "CONTROLS_GATEWAY_RPC"

type ttsGrant struct {
	subject string
	account string
}

func TestTTSGrantIsolation(t *testing.T) {
	catalog := loadRPCCatalog(t)
	for _, grant := range []ttsGrant{
		{"rpc.internal.users.voice.overlay.resolve", controlsGatewayAccount},
		{"rpc.internal.users.voice.overlay.resolve.node.n1", controlsGatewayAccount},
		{"rpc.internal.users.voice.overlay.check", controlsGatewayAccount},
		{"rpc.internal.users.voice.overlay.check.node.n1", controlsGatewayAccount},
		{"controls.tts.revoked.42", controlsGatewayAccount},
		{"rpc.internal.users.voice.check", "WORKER_RPC"},
		{"rpc.internal.users.voice.check.node.n1", "WORKER_RPC"},
		{"rpc.internal.users.voice.record", "WORKER_RPC"},
		{"rpc.internal.users.voice.record.node.n1", "WORKER_RPC"},
	} {
		assertTTSGrant(t, catalog, grant)
	}
}

func assertTTSGrant(t *testing.T, catalog rpcCatalog, grant ttsGrant) {
	t.Helper()
	users := catalog.byUser["users_rpc"]
	match := func(candidate string) bool { return server.SubjectsCollide(candidate, grant.subject) }
	index := slices.IndexFunc(users.exports, func(export rpcGrant) bool { return match(export.subject) })
	require.NotEqual(t, -1, index, "Users does not export %s", grant.subject)
	export := users.exports[index]
	assert.Equal(t, []string{grant.account}, export.accounts)

	for _, account := range catalog.accounts {
		got := slices.IndexFunc(account.imports, func(imp rpcImport) bool { return match(imp.subject) })
		if account.name != grant.account {
			assert.Equal(t, -1, got, "%s imports %s", account.name, grant.subject)
			continue
		}
		require.NotEqual(t, -1, got, "%s must import %s", account.name, grant.subject)
		want := rpcImport{rpcGrant: rpcGrant{kind: export.kind, subject: export.subject}, account: users.name}
		assert.Equal(t, want, account.imports[got])
	}
}

func TestTTSOverlayReaderPermissionsAreExact(t *testing.T) {
	account := committedACL(t).Accounts[controlsGatewayAccount]
	assert.NotNil(t, account.JetStream)
	assert.Empty(t, account.Exports)
	require.Len(t, account.Imports, 5)

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

func TestTTSWriterPermissionsAreExact(t *testing.T) {
	acl := committedACL(t)
	role := acl.Accounts[controlsGatewayAccount].Roles["worker_tts_bus"]
	assert.Equal(t, natsacl.RoleSpec{
		Publish: &natsacl.PermissionSpec{Allow: []string{
			"overlay.tts.*",
			"$O.TTS_CLIPS.C.*",
			"$O.TTS_CLIPS.M.*",
			"$JS.API.STREAM.INFO.OVERLAY_TTS",
			"$JS.API.STREAM.CREATE.OVERLAY_TTS",
			"$JS.API.STREAM.UPDATE.OVERLAY_TTS",
			"$JS.API.STREAM.INFO.OBJ_TTS_CLIPS",
			"$JS.API.STREAM.CREATE.OBJ_TTS_CLIPS",
			"$JS.API.STREAM.UPDATE.OBJ_TTS_CLIPS",
			"$JS.API.STREAM.PURGE.OBJ_TTS_CLIPS",
			"$JS.API.DIRECT.GET.OBJ_TTS_CLIPS.$O.TTS_CLIPS.M.*",
		}},
		Subscribe: &natsacl.PermissionSpec{Allow: []string{"_INBOX.>"}},
	}, role)
	for name, block := range busUserBlocks(t) {
		assert.Equal(t, name == "worker_bus", block.grants("tts.jobs.*"))
	}
}

func TestTTSWorkerAndGatewayShareTheOutputAccount(t *testing.T) {
	t.Setenv("NATS_JS_DOMAIN", "hub")
	s := startLiveACLServer(t, committedACL(t), true)
	jobs := s.connectRole(t, roleEndpoint{"BUS", "worker_bus"})
	_, err := bus.OpenTTSJobs(t.Context(), jobs, 1)
	require.NoError(t, err)

	writer := s.connectRole(t, roleEndpoint{controlsGatewayAccount, "worker_tts_bus"})
	overlay, err := bus.OpenTTSOverlay(t.Context(), writer, 1)
	require.NoError(t, err)
	clip, err := overlay.PutClip(t.Context(), bus.TTSClipData{
		TTSClip: bus.TTSClip{Key: "42/alert"}, WAV: []byte("wav"),
	})
	require.NoError(t, err)

	stored, err := overlay.GetClip(t.Context(), clip)
	require.NoError(t, err)
	assert.Equal(t, clip.Key, stored.Key)
	require.NoError(t, overlay.Publish(t.Context(), bus.TTSMessage{
		BroadcasterID: "42", EventID: "alert", Value: map[string]string{"clip_key": clip.Key},
	}))
	assertTTSClipVisibleToGateway(t, s, stored)
}

func assertTTSClipVisibleToGateway(t *testing.T, s liveACLServer, clip bus.TTSClip) {
	t.Helper()
	gateway := s.connectRole(t, roleEndpoint{controlsGatewayAccount, "controls_gateway_rpc"})
	subject := "$O.TTS_CLIPS.M." + base64.URLEncoding.EncodeToString([]byte(clip.Key))
	query, err := codec.Marshal(map[string]string{"last_by_subj": subject})
	require.NoError(t, err)
	reply, err := gateway.Request("$JS.API.STREAM.MSG.GET.OBJ_TTS_CLIPS", query, time.Second)
	require.NoError(t, err)

	var result struct {
		Message struct {
			Subject string `json:"subject"`
		} `json:"message"`
	}
	require.NoError(t, codec.Unmarshal(reply.Data, &result))
	assert.Equal(t, subject, result.Message.Subject)
}
