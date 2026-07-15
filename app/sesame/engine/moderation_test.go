package engine

import (
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// banBody mirrors the Helix Ban User request body for decoding in tests.
type banBodyWire struct {
	Data struct {
		UserID   string `json:"user_id"`
		Duration int    `json:"duration"`
		Reason   string `json:"reason"`
	} `json:"data"`
}

func TestBuildOutgressBanOmitsDuration(t *testing.T) {
	body, err := buildOutgress(&module.Output{
		Type:          outgress.TypeBan,
		BroadcasterID: "77",
		TargetUserID:  "999",
		Reason:        "hate raid",
	})
	require.NoError(t, err)

	var msg outgress.Message
	require.NoError(t, sonic.Unmarshal(body, &msg))
	assert.Equal(t, outgress.TypeBan, msg.Type)
	assert.Equal(t, "77", msg.BroadcasterID)

	var got banBodyWire
	require.NoError(t, sonic.Unmarshal(msg.Payload, &got))
	assert.Equal(t, "999", got.Data.UserID)
	assert.Equal(t, "hate raid", got.Data.Reason)
	assert.Equal(t, 0, got.Data.Duration, "a permanent ban must omit duration")
	assert.NotContains(t, string(msg.Payload), "duration")
}

func TestBuildOutgressTimeoutCarriesDuration(t *testing.T) {
	body, err := buildOutgress(&module.Output{
		Type:          outgress.TypeTimeout,
		BroadcasterID: "77",
		TargetUserID:  "999",
		Duration:      600,
		Reason:        "spam",
	})
	require.NoError(t, err)

	var msg outgress.Message
	require.NoError(t, sonic.Unmarshal(body, &msg))
	assert.Equal(t, outgress.TypeTimeout, msg.Type)

	var got banBodyWire
	require.NoError(t, sonic.Unmarshal(msg.Payload, &got))
	assert.Equal(t, "999", got.Data.UserID)
	assert.Equal(t, 600, got.Data.Duration)
	assert.Equal(t, "spam", got.Data.Reason)
}

// A folded duplicate cohort (senders present) is plain chat the ingress squash
// collapsed; command dispatch must be skipped even when the text looks like a
// command, while an identical line WITHOUT senders still dispatches.
func TestProcessCohortSkipsCommandDispatch(t *testing.T) {
	reader := fakeReader{
		cmd:      projection.Command{Name: "hi", Response: "hello", IsActive: true},
		cmdFound: true,
	}

	cohort, err := sonic.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"text":                "!hi",
		"senders": []map[string]any{
			{"chatter_user_id": "1"},
			{"chatter_user_id": "2"},
		},
	})
	require.NoError(t, err)

	pub := &fakePublisher{}
	p := newPipelineWith(pub, reader)
	require.NoError(t, p.Process(bus.NewMessage("u1", cohort)))
	assert.Empty(t, pub.got, "a cohort must never dispatch a command")

	// Control: the same command line without senders dispatches and emits.
	pub2 := &fakePublisher{}
	p2 := newPipelineWith(pub2, reader)
	require.NoError(t, p2.Process(chatMsg(t, "standard", "!hi")))
	assert.Len(t, pub2.got, 1, "a normal command line still dispatches")
}
