package engine

import (
	"testing"

	"ItsBagelBot/app/sesame/module"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chatEvent is chatMsg with separate EventSub delivery and Twitch chat IDs.
func chatEvent(t *testing.T, laneName, text, eventID string) *message.Message {
	t.Helper()
	body, err := sonic.Marshal(map[string]any{
		"type":                chatType,
		"lane":                laneName,
		"broadcaster_user_id": "123",
		"chatter_user_id":     "999",
		"text":                text,
		"event_id":            eventID,
		"msg_id":              "chat-message-1",
	})
	require.NoError(t, err)
	return message.NewMessage("uuid-"+eventID, body)
}

func TestReplayUsesSameOutputID(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "pong"))

	require.NoError(t, p.Process(chatEvent(t, "standard", "hi", "event-1")))
	require.NoError(t, p.Process(chatEvent(t, "standard", "hi", "event-1")))

	require.Len(t, pub.got, 2) // the broker, not the engine, folds the replay
	require.NotEmpty(t, pub.got[0].id)
	assert.Equal(t, pub.got[0].id, pub.got[1].id)
}

func TestDistinctEventsUseDistinctOutputIDs(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "pong"))

	require.NoError(t, p.Process(chatEvent(t, "standard", "hi", "event-1")))
	require.NoError(t, p.Process(chatEvent(t, "standard", "hi", "event-2")))

	require.Len(t, pub.got, 2)
	assert.NotEqual(t, pub.got[0].id, pub.got[1].id)
}

func TestEventWithoutIDUsesOrdinaryPublish(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "pong"))

	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))

	require.Len(t, pub.got, 1)
	assert.Empty(t, pub.got[0].id)
}
