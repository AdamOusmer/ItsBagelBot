package pipeline

import (
	"context"
	"testing"

	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeProjection struct {
	user    projection.User
	command projection.Command
	found   bool
}

func (f fakeProjection) User(context.Context, uint64) (projection.User, error) {
	return f.user, nil
}

func (f fakeProjection) Modules(context.Context, uint64) ([]projection.ModuleView, error) {
	return []projection.ModuleView{}, nil
}

func (f fakeProjection) Command(context.Context, uint64, string) (projection.Command, bool, error) {
	return f.command, f.found, nil
}

func TestLiveOnlyCommandIsSuppressedWhenOffline(t *testing.T) {
	p := NewPipeline(zap.NewNop(), nil, fakeProjection{
		user: projection.User{
			Status:   "paid",
			IsActive: true,
			IsLive:   false,
		},
		command: projection.Command{
			Name:             "uptime",
			Response:         "live for a while",
			IsActive:         true,
			StreamOnlineOnly: true,
		},
		found: true,
	}, "premium", "standard")

	out, err := p.handleChatMessage(context.Background(), Envelope{
		Type:              "channel.chat.message",
		BroadcasterUserID: "1001",
		Text:              "!uptime",
	}, RegressPremium)

	require.NoError(t, err)
	assert.Nil(t, out)
}

func TestLiveOnlyCommandRunsWhenLive(t *testing.T) {
	p := NewPipeline(zap.NewNop(), nil, fakeProjection{
		user: projection.User{
			Status:   "paid",
			IsActive: true,
			IsLive:   true,
		},
		command: projection.Command{
			Name:             "uptime",
			Response:         "live for a while",
			IsActive:         true,
			StreamOnlineOnly: true,
		},
		found: true,
	}, "premium", "standard")

	out, err := p.handleChatMessage(context.Background(), Envelope{
		Type:              "channel.chat.message",
		BroadcasterUserID: "1001",
		Text:              "!uptime",
	}, RegressPremium)

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "chat", out.Type)
}
