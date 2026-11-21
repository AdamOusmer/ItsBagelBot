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
	subject  string
	owner    string
	accounts []string
}

func TestTTSGrantIsolation(t *testing.T) {
	catalog := loadRPCCatalog(t)
	worker := []string{"WORKER_RPC"}
	gateway := []string{controlsGatewayAccount}
	overlays := []string{"OVERLAYS_RPC"}
	projector := []string{"PROJECTOR_RPC"}
	quota := []string{"WORKER_RPC", "OVERLAYS_RPC"}
	for _, grant := range []ttsGrant{
		{"rpc.internal.projector.overlays.tts.resolve", "projector_rpc", gateway},
		{"rpc.internal.projector.overlays.tts.resolve.node.n1", "projector_rpc", gateway},
		{"rpc.internal.projector.overlays.tts.check", "projector_rpc", gateway},
		{"rpc.internal.projector.overlays.tts.check.node.n1", "projector_rpc", gateway},
		{"controls.tts.revoked.42", "projector_rpc", gateway},
		{"rpc.internal.projector.overlays.tts.settings", "projector_rpc", worker},
		{"rpc.internal.projector.overlays.tts.settings.node.n1", "projector_rpc", worker},
		{"rpc.internal.users.voice.check", "users_rpc", quota},
		{"rpc.internal.users.voice.check.node.n1", "users_rpc", quota},
		{"rpc.internal.users.voice.record", "users_rpc", worker},
		{"rpc.internal.users.voice.record.node.n1", "users_rpc", worker},
		{"rpc.internal.users.voice.export", "users_rpc", overlays},
		{"rpc.internal.users.voice.export.node.n1", "users_rpc", overlays},
		{"rpc.internal.overlays.tts.by_user", "overlays_rpc", projector},
		{"rpc.internal.overlays.tts.by_user.node.n1", "overlays_rpc", projector},
		{"rpc.internal.overlays.tts.by_hash", "overlays_rpc", projector},
		{"rpc.internal.overlays.tts.by_hash.node.n1", "overlays_rpc", projector},
	} {
		assertTTSGrant(t, catalog, grant)
	}
}

func assertTTSGrant(t *testing.T, catalog rpcCatalog, grant ttsGrant) {
	t.Helper()
	owner := catalog.byUser[grant.owner]
	match := func(candidate string) bool { return server.SubjectsCollide(candidate, grant.subject) }
	index := slices.IndexFunc(owner.exports, func(export rpcGrant) bool { return match(export.subject) })
	require.NotEqual(t, -1, index, "Owner does not export %s", grant.subject)
	export := owner.exports[index]
	assert.ElementsMatch(t, grant.accounts, export.accounts)

	for _, account := range catalog.accounts {
		got := slices.IndexFunc(account.imports, func(imp rpcImport) bool { return match(imp.subject) })
		if !slices.Contains(grant.accounts, account.name) {
			assert.Equal(t, -1, got, "%s imports %s", account.name, grant.subject)
			continue
		}
		require.NotEqual(t, -1, got, "%s must import %s", account.name, grant.subject)
		want := rpcImport{rpcGrant: rpcGrant{kind: export.kind, subject: export.subject}, account: owner.name}
		assert.Equal(t, want, account.imports[got])
	}
}

func TestTTSOverlayReaderPermissionsAreExact(t *testing.T) {
	account := committedACL(t).Accounts[controlsGatewayAccount]
	assert.NotNil(t, account.JetStream)
	assert.Empty(t, account.Exports)
	require.Len(t, account.Imports, 7)

	role := account.Roles["controls_gateway_rpc"]
	assert.Equal(t, natsacl.RoleSpec{
		Publish: &natsacl.PermissionSpec{Allow: []string{
			"rpc.internal.projector.overlays.tts.resolve",
			"rpc.internal.projector.overlays.tts.resolve.node.*",
			"rpc.internal.projector.overlays.tts.check",
			"rpc.internal.projector.overlays.tts.check.node.*",
			"rpc.internal.projector.overlays.powered.resolve",
			"rpc.internal.projector.overlays.powered.resolve.node.*",
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
