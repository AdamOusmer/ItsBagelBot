// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

type AutomationRequest struct {
	Subject string
	UserID  uint64
	Timeout time.Duration
}

func FetchAutomations(ctx context.Context, nc *nats.Conn, request AutomationRequest) ([]automation.Automation, error) {
	items := make([]automation.Automation, 0)
	for page := 1; page > 0; {
		req := automationrpc.ProjectionRequest{UserID: strconv.FormatUint(request.UserID, 10), Page: page}
		reply, err := bus.RequestJSONTimeout[automationrpc.AutomationsProjectionReply](ctx, nc, request.Subject, req, request.Timeout)
		if err != nil {
			return nil, err
		}
		items = append(items, reply.Automations...)
		page = reply.NextPage
	}
	return items, nil
}
