package builtin

import (
	"encoding/json"
	"testing"

	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/require"
)

// replyView is the decoded body of a chat outgress.Message, for assertions.
type replyView struct {
	BroadcasterID string `json:"broadcaster_id"`
	Message       string `json:"message"`
}

func decodeReplies(t *testing.T, msgs []*outgress.Message) []*replyView {
	t.Helper()
	out := make([]*replyView, 0, len(msgs))
	for _, m := range msgs {
		require.NotNil(t, m)
		require.Equal(t, outgress.TypeChat, m.Type)
		var rv replyView
		require.NoError(t, json.Unmarshal(m.Payload, &rv))
		out = append(out, &rv)
	}
	return out
}
