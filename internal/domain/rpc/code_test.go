// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
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

// The vocabulary is a wire contract shared with the console, which asserts the
// same list. Pinning it here means renaming a code fails in Go before it fails
// as a silently-unhandled branch in a Svelte page.
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

// oldReply is a reader built before `code` existed: the shape every console
// and Go caller decoded until this change.
type oldReply struct {
	Value string `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

// newReply is the same reply once it embeds the Refusal.
type newReply struct {
	Value string `json:"value,omitempty"`
	Refusal
}

// Both directions of the rollout have to work while services and readers are
// on mixed builds: a new service answering an old reader, and an old service
// answering a new one.
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

// A success reply must not grow an empty `code` key: readers that treat any
// present `error`/`code` key as a refusal would start refusing every call.
func TestSuccessOmitsBothFields(t *testing.T) {
	if got := mustEncode(t, newReply{Value: "ok"}); got != `{"value":"ok"}` {
		t.Errorf("success reply = %s, want {\"value\":\"ok\"}", got)
	}
}

func mustEncode(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

func decode(t *testing.T, raw string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), into); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
}
