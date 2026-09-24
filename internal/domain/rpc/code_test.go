// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"ItsBagelBot/pkg/codec"
)

var errSentinel = errors.New("bound elsewhere")

func TestFailClassifies(t *testing.T) {
	rules := []Rule{
		Is(errSentinel, CodeConflict),
		When(func(err error) bool { return err.Error() == "gone" }, CodeNotFound),
	}
	cases := map[string]struct {
		err  error
		want Refusal
	}{
		"nil":       {nil, Refusal{}},
		"sentinel":  {fmt.Errorf("save: %w", errSentinel), Refusal{Error: "save: bound elsewhere", Code: CodeConflict}},
		"predicate": {errors.New("gone"), Refusal{Error: "gone", Code: CodeNotFound}},
		"deadline":  {fmt.Errorf("query: %w", context.DeadlineExceeded), Refusal{Error: "query: context deadline exceeded", Code: CodeUnavailable}},
		"unknown":   {errors.New("boom"), Refusal{Error: "boom", Code: CodeInternal}},
	}
	for name, tc := range cases {
		if got := Fail(tc.err, rules...); got != tc.want {
			t.Errorf("%s: Fail = %+v, want %+v", name, got, tc.want)
		}
	}
}

func TestCodesVocabulary(t *testing.T) {
	want := []Code{"invalid", "not_found", "forbidden", "conflict", "unavailable", "internal"}
	got := Codes()
	if len(got) != len(want) {
		t.Fatalf("Codes() = %v, want %v", got, want)
	}
	for i, code := range want {
		if got[i] != code {
			t.Fatalf("Codes()[%d] = %q, want %q", i, got[i], code)
		}
	}
}

type oldReply struct {
	Value string `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

type newReply struct {
	Value string `json:"value,omitempty"`
	Refusal
}

func TestWireCompatBothDirections(t *testing.T) {
	fromNew := mustEncode(t, newReply{Refusal: Refused(CodeNotFound, "no such user")})
	var old oldReply
	decode(t, fromNew, &old)
	if old.Error != "no such user" {
		t.Errorf("old reader lost the message: %+v (from %s)", old, fromNew)
	}

	fromOld := mustEncode(t, oldReply{Error: "no such user"})
	var fresh newReply
	decode(t, fromOld, &fresh)
	if fresh.Error != "no such user" || fresh.Code != CodeOK {
		t.Errorf("new reader mishandled an old reply: %+v (from %s)", fresh, fromOld)
	}
}

func TestSuccessOmitsBothFields(t *testing.T) {
	if got := mustEncode(t, newReply{Value: "ok"}); got != `{"value":"ok"}` {
		t.Errorf("success reply = %s, want {\"value\":\"ok\"}", got)
	}
}

func mustEncode(t *testing.T, v any) string {
	t.Helper()
	raw, err := codec.MarshalToString(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func decode(t *testing.T, raw string, into any) {
	t.Helper()
	if err := codec.UnmarshalFromString(raw, into); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
}
