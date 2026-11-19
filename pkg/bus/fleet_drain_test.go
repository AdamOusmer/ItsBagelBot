// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type drainingLane struct {
	Subscriber
	ctx     context.Context
	started chan<- struct{}
	err     error
}

func (s drainingLane) Close() error {
	s.started <- struct{}{}
	<-s.ctx.Done()
	return s.err
}

func TestFleetCloseDrainsLanesTogetherAndWaitsForEveryResult(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "one failure"}[failed], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			started := make(chan struct{}, 2)
			var err error
			if failed {
				err = errors.New("lane drain failed")
			}
			fleet := &fleetSubscriber{subs: []Subscriber{
				drainingLane{ctx: ctx, started: started, err: err},
				drainingLane{ctx: ctx, started: started},
			}}
			closed := make(chan error, 1)
			go func() { closed <- fleet.Close() }()
			require.Eventually(t, func() bool { return len(started) == 2 }, time.Second, time.Millisecond)
			select {
			case <-closed:
				t.Fatal("fleet closed before all lanes drained")
			default:
			}
			cancel()
			require.Equal(t, failed, <-closed != nil)
			require.NoError(t, fleet.Close())
		})
	}
}
