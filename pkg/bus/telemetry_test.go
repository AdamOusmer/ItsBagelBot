// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/testnats"

	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRPCTraceContextCrossesTheWire(t *testing.T) {
	app := newLocalApplication(t)
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	require.NoError(t, QueueSubscribeJSON(nc, subject, "svc", time.Second, app, zap.NewNop(),
		func(ctx context.Context, _ map[string]string) map[string]string {
			return map[string]string{"trace": newrelic.FromContext(ctx).GetTraceMetadata().TraceID}
		}))
	caller := app.StartTransaction("caller")
	defer caller.End()

	reply, err := RequestJSON[map[string]string](newrelic.NewContext(context.Background(), caller), nc, subject, map[string]string{})

	require.NoError(t, err)
	assert.NotEmpty(t, reply["trace"])
	assert.Equal(t, caller.GetTraceMetadata().TraceID, reply["trace"], "the handler must continue the caller's distributed trace")
}
