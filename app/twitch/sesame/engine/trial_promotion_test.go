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

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"

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

func seedTrial(t *testing.T, vc valkey.Client, id, state string, fields ...string) {
	t.Helper()
	ctx := context.Background()
	args := append([]string{"state", state, "generation", "5"}, fields...)
	cmd := vc.B().Hset().Key("trial:channel:" + id).FieldValue()
	for i := 0; i < len(args); i += 2 {
		cmd = cmd.FieldValue(args[i], args[i+1])
	}
	require.NoError(t, vc.Do(ctx, cmd.Build()).Error())
	require.NoError(t, vc.Do(ctx, vc.B().Zadd().Key("trial:history").ScoreMember().ScoreMember(1, id).Build()).Error())
}

func TestTrialPromotionCarriesCountersOnceIntoTheChannel(t *testing.T) {
	vc := localValkey(t)
	seedTrial(t, vc, "4242", "promoted", "decoded", "120", "answered", "7")
	seedTrial(t, vc, "5353", "removed", "decoded", "80")
	pub := &rawCapture{}
	promo := NewTrialPromotion(vc, pub, zap.NewNop())

	promo.Sweep(context.Background())
	promo.Sweep(context.Background())

	require.Len(t, pub.got, 1)
	require.Equal(t, data.SubjectLoyaltyCounters, pub.got[0].subject)
	require.Equal(t, "trial-promote:4242:5", pub.got[0].id)
	var dto data.CounterBumpedDTO
	require.NoError(t, codec.Unmarshal(pub.got[0].payload, &dto))
	require.Equal(t, uint64(4242), dto.UserID)
	require.ElementsMatch(t, []data.CounterBumpEntry{
		{Name: counterEventsProcessed, Scope: data.CounterScopeChannel, Delta: 120},
		{Name: counterMessagesProcessed, Scope: data.CounterScopeChannel, Delta: 120},
		{Name: counterCommandsAnswered, Scope: data.CounterScopeChannel, Delta: 7},
	}, dto.Bumps)
	done, err := vc.Do(context.Background(), vc.B().Hget().Key("trial:channel:4242").Field("counters_promoted").Build()).ToString()
	require.NoError(t, err)
	require.Equal(t, trialPromotedDone, done)
}

func TestTrialPromotionRetriesAStaleClaimAfterAFailedPublish(t *testing.T) {
	vc := localValkey(t)
	seedTrial(t, vc, "4242", "promoted", "decoded", "3")
	failing := &rawCapture{fail: true}
	NewTrialPromotion(vc, failing, zap.NewNop()).Sweep(context.Background())
	require.Empty(t, failing.got)

	pub := &rawCapture{}
	promo := NewTrialPromotion(vc, pub, zap.NewNop())
	promo.Sweep(context.Background())
	require.Empty(t, pub.got, "a fresh claim is not retried")

	stale := strconv.FormatInt(time.Now().Add(-trialClaimTimeout-time.Second).Unix(), 10)
	require.NoError(t, vc.Do(context.Background(), vc.B().Hset().Key("trial:channel:4242").FieldValue().FieldValue("counters_promoted", stale).Build()).Error())
	promo.Sweep(context.Background())
	require.Len(t, pub.got, 1)
}

type rawCaptured struct {
	subject, id string
	payload     []byte
}

type rawCapture struct {
	fail bool
	got  []rawCaptured
}

func (p *rawCapture) PublishOwned(ctx context.Context, subject string, payload []byte) error {
	return p.PublishOwnedWithID(ctx, subject, "", payload)
}

func (p *rawCapture) PublishOwnedWithID(_ context.Context, subject, id string, payload []byte) error {
	if p.fail {
		return context.DeadlineExceeded
	}
	p.got = append(p.got, rawCaptured{subject: subject, id: id, payload: append([]byte(nil), payload...)})
	return nil
}

func (p *rawCapture) Flush(context.Context) error { return nil }
func (p *rawCapture) Close() error                { return nil }
