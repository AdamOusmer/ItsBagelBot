// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package consumer_test

import (
	"context"
	"sync"
	"testing"

	"ItsBagelBot/app/twitch/sesame/internal/consumer"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type subjectRecorder struct {
	mu       sync.Mutex
	subjects []string
}

func (r *subjectRecorder) Subscribe(ctx context.Context, subject string) (<-chan *bus.Message, error) {
	r.mu.Lock()
	r.subjects = append(r.subjects, subject)
	r.mu.Unlock()
	ch := make(chan *bus.Message)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (r *subjectRecorder) Close() error { return nil }

func (r *subjectRecorder) subscribed() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.subjects...)
}

func TestStartSubscribesHotLanesAndRetryLanesOnlyWithFlowControl(t *testing.T) {
	hot := []string{"twitch.ingress.event.premium", "twitch.ingress.event.standard"}
	retry := []string{"twitch.ingress.retry.premium", "twitch.ingress.retry.standard"}
	tests := []struct {
		name string
		flow string
		want []string
	}{
		{"drains retry lanes alongside the hot lanes with flow control on", "on", append(append([]string(nil), hot...), retry...)},
		{"binds only the hot lanes with flow control unset", "", hot},
		{"binds only the hot lanes with flow control off", "off", hot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NATS_CONSUME_FLOW", tt.flow)
			sub := &subjectRecorder{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c := consumer.New(sub, nil, consumer.Config{
				Lanes:          consumer.Lanes{PremiumSubject: hot[0], StandardSubject: hot[1]},
				Policy:         bus.ScalePolicy{MinConsumers: 1, MaxConsumers: 1, MinRoutines: 1, MaxRoutines: 1},
				PremiumReserve: 25,
			}, zap.NewNop())
			_, err := c.Start(ctx, func(*bus.Message) error { return nil })

			require.NoError(t, err)
			require.ElementsMatch(t, tt.want, sub.subscribed())
		})
	}
}
