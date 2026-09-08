// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"maps"
	"testing"
)

type idReq struct{ UserID string }

func (r idReq) Requested() string { return r.UserID }

type idRep struct {
	ID    uint64
	Error string
}

func (r *idRep) Failed(message string) { r.Error = message }

// TestUserIDRefusals pins the two wire strings this guard answers with. They
// are what a caller reads out of an RPC reply's error field, so collapsing the
// sixteen hand-copied spellings onto these two only stays a collapse if the
// bytes are held to a number.
func TestUserIDRefusals(t *testing.T) {
	got := map[string]string{
		"empty":       refusal(t, ""),
		"not numeric": refusal(t, "abc"),
		"negative":    refusal(t, "-1"),
		"overflow":    refusal(t, "99999999999999999999999"),
	}
	want := map[string]string{
		"empty":       "bad request",
		"not numeric": "invalid user_id",
		"negative":    "invalid user_id",
		"overflow":    "invalid user_id",
	}
	if !maps.Equal(got, want) {
		t.Fatalf("refusals = %v, want %v", got, want)
	}
}

func refusal(t *testing.T, raw string) string {
	t.Helper()
	_, err := UserID(raw)
	if err == nil {
		t.Fatalf("UserID(%q) accepted", raw)
	}
	return err.Error()
}

// TestForUserGuard covers the skeleton: a valid id reaches load with the
// parsed value, and a load error lands in the reply's error field rather than
// escaping as a transport failure.
func TestForUserGuard(t *testing.T) {
	handle := ForUser[idReq, idRep](func(_ context.Context, _ idReq, id uint64) (idRep, error) {
		if id == 7 {
			return idRep{}, errLoad
		}
		return idRep{ID: id}, nil
	})

	got := map[string]idRep{
		"parsed":     handle(context.Background(), idReq{UserID: "42"}),
		"load error": handle(context.Background(), idReq{UserID: "7"}),
		"refused":    handle(context.Background(), idReq{UserID: "x"}),
	}
	want := map[string]idRep{
		"parsed":     {ID: 42},
		"load error": {Error: "load failed"},
		"refused":    {Error: "invalid user_id"},
	}
	if !maps.Equal(got, want) {
		t.Fatalf("guard = %v, want %v", got, want)
	}
}

var errLoad = errStr("load failed")

type errStr string

func (e errStr) Error() string { return string(e) }
