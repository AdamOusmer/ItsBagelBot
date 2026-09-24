// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	valkey "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func localValkey(t *testing.T) valkey.Client {
	t.Helper()
	if _, err := exec.LookPath("valkey-server"); err != nil {
		t.Skip("valkey-server unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	dir := t.TempDir()
	cmd := exec.Command("valkey-server", "--port", strconv.Itoa(port), "--bind", "127.0.0.1", "--save", "", "--appendonly", "no", "--dir", dir)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait(); _ = os.RemoveAll(filepath.Clean(dir)) })
	var client valkey.Client
	require.Eventually(t, func() bool {
		client, err = valkey.NewClient(valkey.ClientOption{InitAddress: []string{"127.0.0.1:" + strconv.Itoa(port)}, DisableCache: true})
		return err == nil
	}, 3*time.Second, 20*time.Millisecond)
	t.Cleanup(client.Close)
	return client
}

type seededTrial struct {
	id     string
	state  string
	fields map[string]string
}

func seedTrial(t *testing.T, vc valkey.Client, row seededTrial) {
	t.Helper()
	ctx := context.Background()
	cmd := vc.B().Hset().Key("trial:channel:"+row.id).FieldValue().FieldValue("state", row.state).FieldValue("generation", "5")
	for field, value := range row.fields {
		cmd = cmd.FieldValue(field, value)
	}
	require.NoError(t, vc.Do(ctx, cmd.Build()).Error())
	require.NoError(t, vc.Do(ctx, vc.B().Zadd().Key("trial:history").ScoreMember().ScoreMember(1, row.id).Build()).Error())
}

type fakePromoter struct {
	fail bool
	got  []uint64
}

func (f *fakePromoter) PromoteTrial(_ context.Context, id uint64) error {
	if f.fail {
		return context.DeadlineExceeded
	}
	f.got = append(f.got, id)
	return nil
}

func TestTrialPromotionPromotesEachPromotedTrialOnce(t *testing.T) {
	vc := localValkey(t)
	seedTrial(t, vc, seededTrial{id: "4242", state: "promoted"})
	seedTrial(t, vc, seededTrial{id: "5353", state: "removed"})
	promoter := &fakePromoter{}
	promo := NewTrialPromotion(vc, promoter, zap.NewNop())

	promo.Sweep(context.Background())
	promo.Sweep(context.Background())

	require.Equal(t, []uint64{4242}, promoter.got)
	done, err := vc.Do(context.Background(), vc.B().Hget().Key("trial:channel:4242").Field("counters_promoted").Build()).ToString()
	require.NoError(t, err)
	require.Equal(t, trialPromotedDone, done)
}

func TestTrialPromotionRetriesAStaleClaimAfterAFailedCall(t *testing.T) {
	vc := localValkey(t)
	seedTrial(t, vc, seededTrial{id: "4242", state: "promoted"})
	NewTrialPromotion(vc, &fakePromoter{fail: true}, zap.NewNop()).Sweep(context.Background())

	promoter := &fakePromoter{}
	promo := NewTrialPromotion(vc, promoter, zap.NewNop())
	promo.Sweep(context.Background())
	require.Empty(t, promoter.got, "a fresh claim is not retried")

	stale := strconv.FormatInt(time.Now().Add(-trialClaimTimeout-time.Second).Unix(), 10)
	require.NoError(t, vc.Do(context.Background(), vc.B().Hset().Key("trial:channel:4242").FieldValue().FieldValue("counters_promoted", stale).Build()).Error())
	promo.Sweep(context.Background())
	require.Equal(t, []uint64{4242}, promoter.got)
}
