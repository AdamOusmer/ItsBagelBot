package pipeline

import "encoding/json"

// OutgressMessage is the wire contract consumed by the outgress workers (see
// app/outgress/internal/worker.OutgressMessage). The pipeline produces these
// as the result of an event: a Helix request the outgress lane will execute
// under the bot's rate budget.
type OutgressMessage struct {
	Type          string          `json:"type"`           // "chat", "api" or "eventsub"
	BroadcasterID string          `json:"broadcaster_id"` // target channel
	SenderID      string          `json:"sender_id"`      // the bot's user ID
	Endpoint      string          `json:"endpoint"`       // Helix path, e.g. "/helix/chat/messages"
	Method        string          `json:"method"`         // HTTP method
	Payload       json.RawMessage `json:"payload"`        // raw JSON body
}
