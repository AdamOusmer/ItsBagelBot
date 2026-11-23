// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"context"
	"encoding/base64"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/natsacl"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
	recorder := []string{"WORKER_RPC", "GOSSIP_RPC"}
	gateway := []string{controlsGatewayAccount}
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
		{"rpc.internal.users.voice.record", "users_rpc", recorder},
		{"rpc.internal.users.voice.record.node.n1", "users_rpc", recorder},
		{"rpc.internal.overlays.tts.by_user", "overlays_rpc", projector},
		{"rpc.internal.overlays.tts.by_user.node.n1", "overlays_rpc", projector},
		{"rpc.internal.overlays.tts.by_hash", "overlays_rpc", projector},
		{"rpc.internal.overlays.tts.by_hash.node.n1", "overlays_rpc", projector},
		{"rpc.internal.projector.overlays.resolve", "projector_rpc", gateway},
		{"rpc.internal.projector.overlays.resolve.node.n1", "projector_rpc", gateway},
		{"rpc.internal.overlays.feed.snapshot", "overlays_rpc", gateway},
		{"rpc.internal.overlays.feed.snapshot.node.n1", "overlays_rpc", gateway},
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
	require.Len(t, account.Imports, 12)
	assert.Contains(t, account.Imports, natsacl.ImportSpec{Stream: "cache.invalidate.overlays", From: "PROJECTOR_RPC"})

	role := account.Roles["controls_gateway_rpc"]
	assert.Equal(t, natsacl.RoleSpec{
		Publish: &natsacl.PermissionSpec{Allow: []string{
			"rpc.internal.projector.overlays.tts.resolve",
			"rpc.internal.projector.overlays.tts.resolve.node.*",
			"rpc.internal.projector.overlays.tts.check",
			"rpc.internal.projector.overlays.tts.check.node.*",
			"rpc.internal.projector.overlays.powered.resolve",
			"rpc.internal.projector.overlays.powered.resolve.node.*",
			"rpc.internal.projector.overlays.resolve",
			"rpc.internal.projector.overlays.resolve.node.*",
			"rpc.internal.overlays.feed.snapshot",
			"rpc.internal.overlays.feed.snapshot.node.*",
			"$JS.API.CONSUMER.CREATE.OVERLAY_TTS.*.overlay.tts.*",
			"$JS.FC.OVERLAY_TTS.*.*",
			"$JS.FC.*.*.OVERLAY_TTS.*.*",
			"$JS.API.STREAM.MSG.GET.OBJ_TTS_CLIPS",
			"$JS.API.CONSUMER.INFO.OVERLAYS_PREMIUM.overlays-premium",
			"$JS.API.CONSUMER.INFO.OVERLAYS_STANDARD.overlays-standard",
			"$JS.API.CONSUMER.MSG.NEXT.OVERLAYS_PREMIUM.overlays-premium",
			"$JS.API.CONSUMER.MSG.NEXT.OVERLAYS_STANDARD.overlays-standard",
			"$JS.ACK.OVERLAYS_PREMIUM.overlays-premium.>",
			"$JS.ACK.*.*.OVERLAYS_PREMIUM.overlays-premium.>",
			"$JS.ACK.OVERLAYS_STANDARD.overlays-standard.>",
			"$JS.ACK.*.*.OVERLAYS_STANDARD.overlays-standard.>",
			"overlays.session.>",
			"$JS.API.STREAM.MSG.GET.OBJ_OVERLAYS_MEDIA",
		}},
		Subscribe: &natsacl.PermissionSpec{Allow: []string{"controls.tts.revoked.*", "cache.invalidate.overlays", "overlays.session.>", "_INBOX.>"}},
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
			"overlays.premium.>",
			"overlays.standard.>",
			"$JS.API.STREAM.INFO.OVERLAYS_PREMIUM",
			"$JS.API.STREAM.CREATE.OVERLAYS_PREMIUM",
			"$JS.API.STREAM.UPDATE.OVERLAYS_PREMIUM",
			"$JS.API.STREAM.INFO.OVERLAYS_STANDARD",
			"$JS.API.STREAM.CREATE.OVERLAYS_STANDARD",
			"$JS.API.STREAM.UPDATE.OVERLAYS_STANDARD",
			"$JS.API.CONSUMER.INFO.OVERLAYS_PREMIUM.overlays-premium",
			"$JS.API.CONSUMER.CREATE.OVERLAYS_PREMIUM.overlays-premium",
			"$JS.API.CONSUMER.DURABLE.CREATE.OVERLAYS_PREMIUM.overlays-premium",
			"$JS.API.CONSUMER.INFO.OVERLAYS_STANDARD.overlays-standard",
			"$JS.API.CONSUMER.CREATE.OVERLAYS_STANDARD.overlays-standard",
			"$JS.API.CONSUMER.DURABLE.CREATE.OVERLAYS_STANDARD.overlays-standard",
			"$O.OVERLAYS_MEDIA.C.*",
			"$O.OVERLAYS_MEDIA.M.*",
			"$JS.API.STREAM.INFO.OBJ_OVERLAYS_MEDIA",
			"$JS.API.STREAM.CREATE.OBJ_OVERLAYS_MEDIA",
			"$JS.API.STREAM.UPDATE.OBJ_OVERLAYS_MEDIA",
			"$JS.API.STREAM.PURGE.OBJ_OVERLAYS_MEDIA",
			"$JS.API.DIRECT.GET.OBJ_OVERLAYS_MEDIA.$O.OVERLAYS_MEDIA.M.*",
			"gossip.jobs.tts.*",
			"$JS.API.STREAM.INFO.GOSSIP_JOBS",
			"$JS.API.STREAM.CREATE.GOSSIP_JOBS",
			"$JS.API.STREAM.UPDATE.GOSSIP_JOBS",
			"$JS.API.CONSUMER.INFO.GOSSIP_JOBS.gossip-jobs",
			"$JS.API.CONSUMER.CREATE.GOSSIP_JOBS.gossip-jobs",
			"$JS.API.CONSUMER.DURABLE.CREATE.GOSSIP_JOBS.gossip-jobs",
		}},
		Subscribe: &natsacl.PermissionSpec{Allow: []string{"_INBOX.>"}},
	}, role)
	for name, block := range busUserBlocks(t) {
		assert.Equal(t, name == "worker_bus", block.grants("tts.jobs.*"))
	}
}

func TestGossipJobsPermissionsAreExact(t *testing.T) {
	role := committedACL(t).Accounts[controlsGatewayAccount].Roles["gossip_jobs_bus"]
	assert.Equal(t, natsacl.RoleSpec{
		Publish: &natsacl.PermissionSpec{Allow: []string{
			"$JS.API.CONSUMER.INFO.GOSSIP_JOBS.gossip-jobs",
			"$JS.API.CONSUMER.MSG.NEXT.GOSSIP_JOBS.gossip-jobs",
			"$JS.ACK.GOSSIP_JOBS.gossip-jobs.>",
			"$JS.ACK.*.*.GOSSIP_JOBS.gossip-jobs.>",
			"$JS.API.STREAM.INFO.GOSSIP_JOBS",
			"overlays.premium.>",
			"$JS.API.STREAM.INFO.OVERLAYS_PREMIUM",
			"$JS.API.STREAM.INFO.OVERLAYS_STANDARD",
			"$O.OVERLAYS_MEDIA.C.*",
			"$O.OVERLAYS_MEDIA.M.*",
			"$JS.API.STREAM.INFO.OBJ_OVERLAYS_MEDIA",
			"$JS.API.STREAM.MSG.GET.OBJ_OVERLAYS_MEDIA",
			"$JS.API.DIRECT.GET.OBJ_OVERLAYS_MEDIA.$O.OVERLAYS_MEDIA.M.*",
		}},
		Subscribe: &natsacl.PermissionSpec{Allow: []string{"_INBOX.>"}},
	}, role)
}

func TestGossipJobsFlowAcrossRoles(t *testing.T) {
	t.Setenv("NATS_JS_DOMAIN", "hub")
	s := startLiveACLServer(t, committedACL(t), true)
	writer := s.connectRole(t, roleEndpoint{controlsGatewayAccount, "worker_tts_bus"})
	_, err := bus.OpenOverlays(t.Context(), writer, 1)
	require.NoError(t, err)
	queue, err := bus.OpenGossipJobs(t.Context(), writer, 1)
	require.NoError(t, err)
	require.NoError(t, queue.Publish(t.Context(), bus.GossipJob{Kind: bus.GossipJobTTS, UserID: "42", EventID: "alert", Value: map[string]string{"text": "hi"}}))

	puller := s.connectRole(t, roleEndpoint{controlsGatewayAccount, "gossip_jobs_bus"})
	jobs, err := bus.BindGossipJobs(t.Context(), puller)
	require.NoError(t, err)
	overlay, err := bus.BindOverlays(t.Context(), puller)
	require.NoError(t, err)
	handled := make(chan bus.GossipJobMessage, 1)
	stop, err := jobs.Consume(t.Context(), func(ctx context.Context, msg bus.GossipJobMessage) error {
		_, err := overlay.PutClip(ctx, bus.OverlayClipData{OverlayClip: bus.OverlayClip{Key: msg.UserID + "/alert"}, WAV: []byte("wav")})
		if err != nil {
			return err
		}
		err = overlay.Publish(ctx, bus.OverlayMessage{Tier: bus.OverlayPremium, Kind: bus.OverlayKindTTS, UserID: msg.UserID, EventID: "alert", Value: map[string]string{"clip_key": msg.UserID + "/alert"}})
		handled <- msg
		return err
	}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(stop)

	msg := <-handled
	assert.Equal(t, bus.GossipJobTTS, msg.Kind)
	gateway := s.connectRole(t, roleEndpoint{controlsGatewayAccount, "controls_gateway_rpc"})
	assertOverlayEventAckedByGateway(t, gateway)
}

func TestTTSWorkerAndGatewayShareTheOutputAccount(t *testing.T) {
	t.Setenv("NATS_JS_DOMAIN", "hub")
	s := startLiveACLServer(t, committedACL(t), true)

	writer := s.connectRole(t, roleEndpoint{controlsGatewayAccount, "worker_tts_bus"})
	overlay, err := bus.OpenOverlays(t.Context(), writer, 1)
	require.NoError(t, err)
	clip, err := overlay.PutClip(t.Context(), bus.OverlayClipData{
		OverlayClip: bus.OverlayClip{Key: "42/alert"}, WAV: []byte("wav"),
	})
	require.NoError(t, err)

	stored, err := overlay.GetClip(t.Context(), clip)
	require.NoError(t, err)
	assert.Equal(t, clip.Key, stored.Key)
	require.NoError(t, overlay.Publish(t.Context(), bus.OverlayMessage{
		Tier: bus.OverlayPremium, Kind: bus.OverlayKindTTS, UserID: "42", EventID: "alert",
		Value: map[string]string{"clip_key": clip.Key},
	}))
	gateway := s.connectRole(t, roleEndpoint{controlsGatewayAccount, "controls_gateway_rpc"})
	assertTTSClipVisibleToGateway(t, gateway, stored)
	assertOverlayEventAckedByGateway(t, gateway)
	assertOverlayOfferAnsweredAcrossGateways(t, gateway, s.connectRole(t, roleEndpoint{controlsGatewayAccount, "controls_gateway_rpc"}))
}

func assertOverlayOfferAnsweredAcrossGateways(t *testing.T, puller, session *nats.Conn) {
	t.Helper()
	offers, err := session.Subscribe("overlays.session.42", func(m *nats.Msg) { _ = m.Respond([]byte(`{"accepted":true}`)) })
	require.NoError(t, err)
	t.Cleanup(func() { _ = offers.Unsubscribe() })
	require.NoError(t, session.Flush())
	replies, err := puller.SubscribeSync("overlays.session.reply.test")
	require.NoError(t, err)
	require.NoError(t, puller.PublishRequest("overlays.session.42", "overlays.session.reply.test", []byte("{}")))
	reply, err := replies.NextMsg(2 * time.Second)
	require.NoError(t, err)
	assert.JSONEq(t, `{"accepted":true}`, string(reply.Data))
}

func assertOverlayEventAckedByGateway(t *testing.T, gateway *nats.Conn) {
	t.Helper()
	next := "$JS.API.CONSUMER.MSG.NEXT." + bus.OverlaysPremiumStream + "." + bus.OverlaysPremiumDurable
	msg, err := gateway.Request(next, []byte(`{"batch":1,"expires":1000000000}`), 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "overlays.premium.tts.42", msg.Subject)
	reply, err := gateway.Request(msg.Reply, []byte("+ACK"), time.Second)
	require.NoError(t, err)
	assert.Empty(t, reply.Data)
}

func assertTTSClipVisibleToGateway(t *testing.T, gateway *nats.Conn, clip bus.OverlayClip) {
	t.Helper()
	subject := "$O." + bus.OverlaysMediaBucket + ".M." + base64.URLEncoding.EncodeToString([]byte(clip.Key))
	query, err := codec.Marshal(map[string]string{"last_by_subj": subject})
	require.NoError(t, err)
	reply, err := gateway.Request("$JS.API.STREAM.MSG.GET.OBJ_"+bus.OverlaysMediaBucket, query, time.Second)
	require.NoError(t, err)

	var result struct {
		Message struct {
			Subject string `json:"subject"`
		} `json:"message"`
	}
	require.NoError(t, codec.Unmarshal(reply.Data, &result))
	assert.Equal(t, subject, result.Message.Subject)
}

var overlayResourceName = regexp.MustCompile(`^[A-Z0-9]+(_[A-Z0-9]+)*$`)

func TestBusOverlayResourcesKeepCreateGrants(t *testing.T) {
	granted := map[string]bool{}
	for _, account := range committedACL(t).Accounts {
		for _, role := range account.Roles {
			for _, subject := range publishAllowed(role.Publish) {
				granted[subject] = true
			}
		}
	}
	names := busOverlayResourceNames(t)
	require.NotEmpty(t, names)
	for _, name := range names {
		stream := granted["$JS.API.STREAM.CREATE."+name]
		bucket := granted["$JS.API.STREAM.CREATE.OBJ_"+name]
		assert.True(t, stream || bucket, "pkg/bus uses %s but accounts.yaml has no create grant for it", name)
	}
}

func publishAllowed(spec *natsacl.PermissionSpec) []string {
	if spec == nil {
		return nil
	}
	return spec.Allow
}

func busOverlayResourceNames(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../pkg/bus/*.go")
	require.NoError(t, err)
	var names []string
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, err)
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err == nil && isOverlayResourceName(value) {
				names = append(names, value)
			}
			return true
		})
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func isOverlayResourceName(value string) bool {
	if !overlayResourceName.MatchString(value) || strings.Contains(value, "BUS") {
		return false
	}
	return strings.HasPrefix(value, "OVERLAY") || strings.HasPrefix(value, "TTS_")
}
