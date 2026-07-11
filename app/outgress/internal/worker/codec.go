package worker

import (
	"bytes"
	"encoding/json"
	"reflect"

	"ItsBagelBot/internal/domain/outgress"

	"github.com/bytedance/sonic"
)

// wireMessage keeps the nested payload as a zero-copy view into Watermill's
// message buffer. The buffer remains owned for the whole synchronous handler.
type wireMessage struct {
	Type          string                 `json:"type"`
	BroadcasterID string                 `json:"broadcaster_id"`
	SenderID      string                 `json:"sender_id"`
	Endpoint      string                 `json:"endpoint"`
	Method        string                 `json:"method"`
	Payload       sonic.NoCopyRawMessage `json:"payload"`
	As            string                 `json:"as,omitempty"`
	Color         string                 `json:"color,omitempty"`
	To            string                 `json:"to,omitempty"`
	MsgID         string                 `json:"msg_id,omitempty"`
	RewardID      string                 `json:"reward_id,omitempty"`
	RedemptionID  string                 `json:"redemption_id,omitempty"`
	Status        string                 `json:"status,omitempty"`
}

// PrepareJSON compiles Sonic's decoders during startup rather than on the first
// latency-sensitive message.
func PrepareJSON() error {
	return sonic.PretouchMany([]reflect.Type{
		reflect.TypeOf(wireMessage{}),
		reflect.TypeOf(outgress.Batch{}),
		reflect.TypeOf(outgress.EventSubJob{}),
		reflect.TypeOf(outgress.StreamStatusJob{}),
	})
}

func decodeMessage(data []byte, destination *outgress.Message) error {
	var wire wireMessage
	if err := sonic.ConfigFastest.Unmarshal(data, &wire); err != nil {
		return err
	}
	*destination = outgress.Message{
		Type: wire.Type, BroadcasterID: wire.BroadcasterID, SenderID: wire.SenderID,
		Endpoint: wire.Endpoint, Method: wire.Method, Payload: json.RawMessage(wire.Payload),
		As: wire.As, Color: wire.Color, To: wire.To, MsgID: wire.MsgID,
		RewardID: wire.RewardID, RedemptionID: wire.RedemptionID, Status: wire.Status,
	}
	return nil
}

func decodeBatch(data []byte, destination *outgress.Batch) error {
	return sonic.ConfigFastest.Unmarshal(data, destination)
}

// withSenderID ensures the chat body carries sender_id without disturbing the
// other fields the producer set. A sender_id already present is left untouched.
func withSenderID(body []byte, senderID string) []byte {
	return withField(body, "sender_id", senderID)
}

// withField inserts "field":"value" into a JSON object body without decoding it.
// If the field already appears, body is returned unchanged. Used to inject the
// bot identity (sender_id / moderator_id) Twitch requires but producers omit, or
// a default announce color, without paying a full marshal/unmarshal round-trip.
//
// Twitch ids/logins and the fixed color/identity values are alphanumeric plus
// underscore, so value needs no JSON escaping; callers pass only such safe
// strings.
func withField(body []byte, field, value string) []byte {
	if bytes.Contains(body, []byte("\""+field+"\"")) {
		return body // already present; leave the producer's value alone
	}

	insert := "\"" + field + "\":\"" + value + "\""

	end := bytes.LastIndexByte(body, '}')
	if end < 0 {
		// No closing '}' to splice into. Only synthesize a fresh object when the
		// body is empty or all whitespace; a non-empty, non-object body (e.g. a
		// top-level JSON array) is not ours to rewrite, so return it unchanged
		// rather than discarding it.
		if len(bytes.TrimSpace(body)) == 0 {
			return []byte("{" + insert + "}")
		}
		return body
	}

	if needsComma(body, end) {
		insert = "," + insert
	}

	out := make([]byte, 0, len(body)+len(insert))
	out = append(out, body[:end]...)
	out = append(out, insert...)
	out = append(out, body[end:]...)
	return out
}

// needsComma reports whether a field spliced in before body[end] follows an
// existing field (so it needs a comma), by finding the previous non-space
// byte: if that is the opening '{' the object is empty and the field goes in
// bare.
func needsComma(body []byte, end int) bool {
	for i := end - 1; i >= 0; i-- {
		switch body[i] {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return body[i] != '{'
		}
	}
	return false
}
