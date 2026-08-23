// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"bytes"
	"testing"

	"ItsBagelBot/pkg/codec"
)

// The RPC paths encode replies with codec.FastMarshal while rpcErrorMessage
// screens them with a raw byte scan for `"error"`. These tests pin that the
// fast encoder emits struct tags and map keys in exactly the shape the probe
// expects, so switching encoders can never silently blind the error check.
func TestFastMarshalErrorEnvelopeHitsProbe(t *testing.T) {
	type errorEnvelope struct {
		Error string `json:"error"`
	}

	for name, body := range map[string]any{
		"struct field": errorEnvelope{Error: "service draining"},
		"map key":      map[string]string{"error": "internal error"},
		"nested payload": struct {
			Result string            `json:"result"`
			Error  string            `json:"error"`
			Detail map[string]string `json:"detail,omitempty"`
		}{Result: "", Error: "bad request", Detail: map[string]string{"code": "E400"}},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := codec.FastMarshal(body)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, errorFieldProbe) {
				t.Fatalf("FastMarshal output %s misses probe %q", data, errorFieldProbe)
			}
			if got := ReplyErrorMessage(data); got != errorMessageOf(t, body) {
				t.Fatalf("ReplyErrorMessage(%s) = %q, want %q", data, got, errorMessageOf(t, body))
			}
		})
	}
}

func errorMessageOf(t *testing.T, body any) string {
	t.Helper()
	data, err := codec.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	msg := ReplyErrorMessage(data)
	if msg == "" {
		t.Fatalf("std-encoded %s also missed by ReplyErrorMessage", data)
	}
	return msg
}

// A reply whose bytes contain `"error"` only as some other object's key must
// stay a success: the scan is a cheap prefilter and the decode decides.
func TestRPCErrorMessageValueFalsePositiveStaysSuccess(t *testing.T) {
	data, err := codec.FastMarshal(map[string]map[string]string{"meta": {"error": "ignored"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, errorFieldProbe) {
		t.Fatalf("fixture %s should contain the probe bytes", data)
	}
	if msg := ReplyErrorMessage(data); msg != "" {
		t.Fatalf("ReplyErrorMessage(%s) = %q, want empty", data, msg)
	}
}

// Escaping differences between std and fast encoding are byte-level only:
// both must decode to identical values on the RPC round trip.
func TestFastCodecRoundTripDecodesIdenticallyToStd(t *testing.T) {
	type payload struct {
		Text string `json:"text"`
		N    int    `json:"n"`
	}
	in := payload{Text: "<bagel> & \"shop\"", N: 7}

	fastData, err := codec.FastMarshal(in)
	if err != nil {
		t.Fatal(err)
	}
	stdData, err := codec.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}

	var fastOut, stdOut payload
	if err := codec.FastUnmarshal(fastData, &fastOut); err != nil {
		t.Fatal(err)
	}
	if err := codec.Unmarshal(stdData, &stdOut); err != nil {
		t.Fatal(err)
	}
	if fastOut != stdOut {
		t.Fatalf("fast round trip %+v != std round trip %+v", fastOut, stdOut)
	}
	if fastOut != in {
		t.Fatalf("round trip lost data: got %+v, want %+v", fastOut, in)
	}
}
