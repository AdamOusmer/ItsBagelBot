// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package kv_test

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func valkeyClient(t *testing.T) valkey.Client {
	t.Helper()
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		addr = startLocalValkey(t)
	}
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, Password: os.Getenv("VALKEY_TEST_PASSWORD")})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func startLocalValkey(t *testing.T) string {
	t.Helper()
	binary, err := exec.LookPath("valkey-server")
	if err != nil {
		t.Skip("VALKEY_TEST_ADDR is not set and valkey-server is not installed")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	cmd := exec.Command(binary, "--port", strconv.Itoa(port), "--bind", "127.0.0.1", "--save", "", "--appendonly", "no", "--dir", t.TempDir())
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	addr := "127.0.0.1:" + strconv.Itoa(port)
	require.Eventually(t, func() bool {
		conn, dialErr := net.Dial("tcp", addr)
		if dialErr != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 5*time.Second, 20*time.Millisecond)
	return addr
}

type stored struct {
	Value string
	TTL   time.Duration
}

func readStored(t *testing.T, client valkey.Client, key string) stored {
	t.Helper()
	ctx := context.Background()
	value, err := client.Do(ctx, client.B().Get().Key(key).Build()).ToString()
	require.NoError(t, err, key)
	ttl, err := client.Do(ctx, client.B().Pttl().Key(key).Build()).AsInt64()
	require.NoError(t, err, key)
	return stored{Value: value, TTL: time.Duration(ttl) * time.Millisecond}
}

func ttlWithin(want time.Duration, got stored) stored {
	if got.TTL > want-time.Minute && got.TTL <= want {
		got.TTL = want
	}
	return got
}

const week = 7 * 24 * time.Hour

func TestKVStoresKeepTheirDocumentedKeysAndFormats(t *testing.T) {
	client := valkeyClient(t)
	ctx := context.Background()
	live, reauth, lockdown := kv.New(client), kv.NewReauthStore(client), kv.NewLockdownStore(client)
	state := kv.LockdownState{
		VerificationLevel: 2, EveryoneRoleID: "e1",
		Channels: []kv.LockdownChannel{{ChannelID: "c1", Allow: "1", Deny: "2"}},
	}

	require.NoError(t, live.PutLiveMessage(ctx, "g1", discapi.Message{ChannelID: "c1", ID: "m1"}))
	require.NoError(t, reauth.MarkNeedsReauth(ctx, "g1"))
	require.NoError(t, lockdown.PutLockdown(ctx, "g1", state))

	require.Equal(t, map[string]stored{
		"discord:live-msg:g1": {Value: "c1|m1", TTL: week},
		"discord:reauth:g1":   {Value: "1", TTL: -1 * time.Millisecond},
		"discord:lockdown:g1": {Value: `{"verification_level":2,"everyone_role_id":"e1","channels":[{"channel_id":"c1","allow":"1","deny":"2"}]}`, TTL: week},
	}, map[string]stored{
		"discord:live-msg:g1": ttlWithin(week, readStored(t, client, "discord:live-msg:g1")),
		"discord:reauth:g1":   readStored(t, client, "discord:reauth:g1"),
		"discord:lockdown:g1": ttlWithin(week, readStored(t, client, "discord:lockdown:g1")),
	})
	gotLive, liveOK := live.GetLiveMessage(ctx, "g1")
	gotState, stateOK := lockdown.GetLockdown(ctx, "g1")
	require.Equal(t, discapi.Message{ChannelID: "c1", ID: "m1"}, gotLive)
	require.True(t, liveOK)
	require.Equal(t, state, gotState)
	require.True(t, stateOK)
	require.True(t, reauth.NeedsReauth(ctx, "g1"))
}

func TestKVStoresForgetWhatTheyAreToldToDelete(t *testing.T) {
	client := valkeyClient(t)
	ctx := context.Background()
	live, reauth, lockdown := kv.New(client), kv.NewReauthStore(client), kv.NewLockdownStore(client)
	require.NoError(t, live.PutLiveMessage(ctx, "g1", discapi.Message{ChannelID: "c1", ID: "m1"}))
	require.NoError(t, reauth.MarkNeedsReauth(ctx, "g1"))
	require.NoError(t, lockdown.PutLockdown(ctx, "g1", kv.LockdownState{VerificationLevel: 1}))

	require.NoError(t, live.DeleteLiveMessage(ctx, "g1"))
	require.NoError(t, reauth.ClearNeedsReauth(ctx, "g1"))
	require.NoError(t, lockdown.DeleteLockdown(ctx, "g1"))

	_, liveOK := live.GetLiveMessage(ctx, "g1")
	_, lockdownOK := lockdown.GetLockdown(ctx, "g1")
	require.False(t, liveOK)
	require.False(t, lockdownOK)
	require.False(t, reauth.NeedsReauth(ctx, "g1"))
}

func TestLiveMessageIgnoresAMalformedStoredValue(t *testing.T) {
	client := valkeyClient(t)
	ctx := context.Background()
	for _, raw := range []string{"garbage", "|m1", "c1|"} {
		require.NoError(t, client.Do(ctx, client.B().Set().Key("discord:live-msg:g1").Value(raw).Build()).Error())

		_, ok := kv.New(client).GetLiveMessage(ctx, "g1")

		require.False(t, ok, raw)
	}
}

func TestBotStatusReaderDecodesTheStoredStatus(t *testing.T) {
	client := valkeyClient(t)
	ctx := context.Background()
	reader := kv.NewBotStatusReader(client)
	want := ddiscord.BotStatus{Connected: true, SessionID: "s1", GuildCount: 3}
	raw, err := ddiscord.EncodeBotStatus(want)
	require.NoError(t, err)

	_, before := reader.BotStatus(ctx)
	require.NoError(t, client.Do(ctx, client.B().Set().Key(ddiscord.BotStatusKey).Value(string(raw)).Build()).Error())
	got, after := reader.BotStatus(ctx)

	require.False(t, before, "no status is published yet")
	require.True(t, after)
	require.Equal(t, want, got)
}
