// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection_test

import (
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchAutomationsFollowsPages(t *testing.T) {
	for _, refused := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "refused"}[refused], func(t *testing.T) {
			nc := bustest.NATS(t)
			pages := make(chan int, 2)
			_, err := nc.Subscribe("automation.pages", func(msg *nats.Msg) {
				var request automationrpc.ProjectionRequest
				require.NoError(t, codec.Unmarshal(msg.Data, &request))
				pages <- request.Page
				reply := automationrpc.AutomationsProjectionReply{Automations: []automation.Automation{{ID: request.UserID}}}
				if request.Page == 1 {
					reply.NextPage = 2
				}
				if refused && request.Page == 2 {
					reply.Error = "owner unavailable"
				}
				assert.NoError(t, bus.Respond(msg, reply))
			})
			require.NoError(t, err)
			items, err := projection.FetchAutomations(t.Context(), nc, projection.AutomationRequest{Subject: "automation.pages", UserID: 7, Timeout: time.Second})
			assert.Equal(t, []int{1, 2}, []int{<-pages, <-pages})
			assert.Equal(t, refused, err != nil)
			if refused {
				assert.Empty(t, items)
				return
			}
			assert.Equal(t, []automation.Automation{{ID: "7"}, {ID: "7"}}, items)
		})
	}
}
