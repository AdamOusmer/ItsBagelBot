// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"bytes"
	"reflect"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

type wireMessage struct {
	Type          string                 `json:"type"`
	BroadcasterID string                 `json:"broadcaster_id"`
	SenderID      string                 `json:"sender_id"`
	Endpoint      string                 `json:"endpoint"`
	Method        string                 `json:"method"`
	Payload       codec.NoCopyRawMessage `json:"payload"`
	As            string                 `json:"as,omitempty"`
	Color         string                 `json:"color,omitempty"`
	To            string                 `json:"to,omitempty"`
	MsgID         string                 `json:"msg_id,omitempty"`
	RewardID      string                 `json:"reward_id,omitempty"`
	RedemptionID  string                 `json:"redemption_id,omitempty"`
	Status        string                 `json:"status,omitempty"`
}

func PrepareJSON() error {
	return codec.Pretouch(
		reflect.TypeOf(wireMessage{}),
		reflect.TypeOf(outgress.Batch{}),
		reflect.TypeOf(outgress.EventSubJob{}),
		reflect.TypeOf(outgress.StreamStatusJob{}),
	)
}

func decodeMessage(data []byte, destination *outgress.Message) error {
	var wire wireMessage
	if err := codec.FastUnmarshal(data, &wire); err != nil {
		return err
	}
	*destination = outgress.Message{
		Type: wire.Type, BroadcasterID: wire.BroadcasterID, SenderID: wire.SenderID,
		Endpoint: wire.Endpoint, Method: wire.Method, Payload: codec.RawMessage(wire.Payload),
		As: wire.As, Color: wire.Color, To: wire.To, MsgID: wire.MsgID,
		RewardID: wire.RewardID, RedemptionID: wire.RedemptionID, Status: wire.Status,
	}
	return nil
}

func decodeBatch(data []byte, destination *outgress.Batch) error {
	return codec.FastUnmarshal(data, destination)
}

func withSenderID(body []byte, senderID string) []byte {
	return withField(body, "sender_id", senderID)
}

// value is not escaped: pass only ids, logins and fixed [A-Za-z0-9_] values.
func withField(body []byte, field, value string) []byte {
	if bytes.Contains(body, []byte("\""+field+"\"")) {
		return body
	}

	insert := "\"" + field + "\":\"" + value + "\""

	end := bytes.LastIndexByte(body, '}')
	if end < 0 {
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
