// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stallThenEmit(text string) module.Module {
	b := module.NewModule("", module.KindCore)
	b.On(chatType, func(ctx context.Context, c *module.Context, emit module.Emit) error {
		<-ctx.Done()
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
		return nil
	})
	return b.Build()
}

func TestProcessBoundsAStalledHandlerAndStillPublishes(t *testing.T) {
	previous := messageBudget
	messageBudget = 50 * time.Millisecond
	t.Cleanup(func() { messageBudget = previous })
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, stallThenEmit("late"))

	started := time.Now()
	err := p.Process(chatMsg(t, "standard", "hi"))

	require.NoError(t, err)
	assert.Less(t, time.Since(started), 2*time.Second)
	assert.Equal(t, []published{{standardSubj, outgress.TypeChat, "", "late"}}, publishedMessages(pub))
}

func TestProcessInheritsTheLaneDeadlineWhenShorter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	msg := chatMsg(t, "standard", "hi")
	msg.SetContext(ctx)
	p := newPipelineWith(&fakePublisher{}, fakeReader{user: projection.User{}}, stallThenEmit("late"))

	started := time.Now()
	require.NoError(t, p.Process(msg))

	assert.Less(t, time.Since(started), 2*time.Second)
}
