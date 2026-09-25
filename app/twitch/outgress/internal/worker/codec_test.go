// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"reflect"
	"testing"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

func TestDecodeMessageKeepsEveryMessageField(t *testing.T) {
	want := outgress.Message{
		Origin: "trial", TrialGeneration: 7, Type: outgress.TypeChat, BroadcasterID: "42", Locale: "fr",
		SenderID: "9", Endpoint: "/helix/x", Method: "POST", Payload: codec.RawMessage(`{"a":1}`),
		As: "app", Color: "blue", To: "7", MsgID: "m", RewardID: "r", RedemptionID: "d", Status: "FULFILLED",
	}
	body, err := codec.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got outgress.Message
	if err := decodeMessage(body, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decode lost fields:\nwant %+v\ngot  %+v", want, got)
	}
	if n := reflect.TypeOf(want).NumField(); n != 16 {
		t.Fatalf("outgress.Message has %d fields; set every field in this test", n)
	}
}
