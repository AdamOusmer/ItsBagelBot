// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type helixRedirect struct{ target *url.URL }

func (r helixRedirect) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host, out.Host = r.target.Scheme, r.target.Host, r.target.Host
	return http.DefaultTransport.RoundTrip(out)
}

func e2eBucket(ctx context.Context, t *testing.T, js jetstream.JetStream, name string) jetstream.KeyValue {
	t.Helper()
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: name, History: 1, TTL: 10 * time.Minute, Storage: jetstream.MemoryStorage})
	require.NoError(t, err)
	return kv
}

// Started by app/twitch/sesame/e2e as a separate process, since outgress's packages are internal to it.
func TestRecipesE2EServe(t *testing.T) {
	natsURL := os.Getenv("RECIPES_E2E_NATS_URL")
	if natsURL == "" {
		t.Skip("RECIPES_E2E_NATS_URL is not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := zap.NewNop()

	vc, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{os.Getenv("VALKEY_TEST_ADDR")}, DisableCache: true})
	require.NoError(t, err)
	defer vc.Close()

	registry := channels.New(vc)
	defer registry.Close()
	registry.SeedPause(false)
	for _, id := range strings.Split(os.Getenv("RECIPES_E2E_CHANNELS"), ",") {
		registry.Prime(manage.Channel{BroadcasterID: id, Enabled: true, IsMod: true})
	}
	for _, id := range strings.Split(os.Getenv("RECIPES_E2E_NONMOD_CHANNELS"), ",") {
		registry.Prime(manage.Channel{BroadcasterID: id, Enabled: true})
	}

	js, closeJS, err := bus.OpenCoordination(natsURL)
	require.NoError(t, err)
	defer closeJS()

	target, err := url.Parse(os.Getenv("RECIPES_E2E_TWITCH_URL"))
	require.NoError(t, err)
	tw := twitch.NewClient("e2e-client-id", twitch.NewStaticTokenSource("app-token"), twitch.NewStaticTokenSource("bot-token"),
		twitch.NewBroadcasterTokens(func(string) *twitch.Source { return twitch.NewStaticTokenSource("broadcaster-token") }))
	tw.SetTransport(helixRedirect{target})

	var limiter ratelimit.Manager = ratelimit.NewJetStreamManager(e2eBucket(ctx, t, js, "outgress_rate"))
	base := Config{
		Log: log, Limiter: limiter, Registry: registry, Twitch: tw, BotID: "987654", Owner: "e2e",
		Batch:           NewJetStreamBatchStore(e2eBucket(ctx, t, js, "outgress_batch")),
		UserIDs:         NewUserIDCache(),
		RewardSnapshots: NewRewardSnapshots(vc),
	}
	premiumCfg, standardCfg := base, base
	premiumCfg.Lane, standardCfg.Lane = LanePremium, LaneStandard
	premium, standard := New(premiumCfg), New(standardCfg)

	const premiumSubject, standardSubject = "twitch.outgress.premium", "twitch.outgress.standard"
	lanes := []bus.WeightedLane{
		{Sub: e2eLane(t, e2eLaneSpec{NatsURL: natsURL, Subject: premiumSubject, Group: "outgress-premium"}, log), Subject: premiumSubject, Handle: premium.Process, Reserve: 50},
		{Sub: e2eLane(t, e2eLaneSpec{NatsURL: natsURL, Subject: standardSubject, Group: "outgress-standard"}, log), Subject: standardSubject, Handle: standard.Process},
	}
	_, err = bus.ConsumeWeighted(ctx, nil, lanes, bus.ScalePolicy{MinRoutines: 4, MaxRoutines: 8, MaxConsumers: 1, ScaleUpAfter: time.Second, ScaleDownAfter: time.Minute}, log)
	require.NoError(t, err)

	fmt.Println("RECIPES_E2E_READY")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

type e2eLaneSpec struct {
	NatsURL string
	Subject string
	Group   string
}

func e2eLane(t *testing.T, spec e2eLaneSpec, log *zap.Logger) bus.Subscriber {
	t.Helper()
	sub, err := bus.NewLaneSubscriber(bus.LaneConfig{
		URL: spec.NatsURL, Stream: bus.OutgressStream.Name, Subject: spec.Subject,
		Group: spec.Group, NakDelay: time.Second, MaxRedeliveries: 3,
	}, log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })
	return sub
}
