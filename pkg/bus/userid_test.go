// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type idReq struct{ UserID string }

func (r idReq) Requested() string { return r.UserID }

type idRep struct {
	ID    uint64
	Error string
}

func (r *idRep) Failed(message string) { r.Error = message }

func TestUserIDRefusals(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"empty", "", "bad request"},
		{"not numeric", "abc", "invalid user_id"},
		{"negative", "-1", "invalid user_id"},
		{"overflow", "99999999999999999999999", "invalid user_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UserID(tc.raw)

			require.Error(t, err)
			assert.EqualError(t, err, tc.want)
		})
	}
}

func TestForUserGuard(t *testing.T) {
	handle := ForUser[idReq, idRep](func(_ context.Context, _ idReq, id uint64) (idRep, error) {
		if id == 7 {
			return idRep{}, errors.New("load failed")
		}
		return idRep{ID: id}, nil
	})

	for _, tc := range []struct {
		name string
		raw  string
		want idRep
	}{
		{"parsed", "42", idRep{ID: 42}},
		{"load error", "7", idRep{Error: "load failed"}},
		{"refused", "x", idRep{Error: "invalid user_id"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, handle(context.Background(), idReq{UserID: tc.raw}))
		})
	}
}
