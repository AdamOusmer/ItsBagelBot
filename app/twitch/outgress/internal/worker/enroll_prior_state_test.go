// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEnrollRetriesLaterWhenThePriorStateCannotBeRead(t *testing.T) {
	for name, enroll := range map[string]func(*Worker, context.Context, enrollment) error{
		"enable":    (*Worker).enableEventSubs,
		"reconnect": (*Worker).reconnectEventSubs,
	} {
		t.Run(name, func(t *testing.T) {
			srv := valkeytest.New(t)
			srv.Fail(valkeytest.Failure{Cmd: "HGETALL", Message: "ERR read refused"})
			w := &Worker{log: zap.NewNop(), registry: channels.New(srv.Client()), owner: "test"}

			err := enroll(w, context.Background(), enrollment{broadcasterID: "42", conduitID: "c"})

			require.Error(t, err, "the job must be redelivered rather than treat the channel as never blocked")
			for _, op := range srv.Ops() {
				assert.NotEqual(t, "HSET", op.Cmd, "no pending write may clobber a state that was never read")
			}
		})
	}
}
