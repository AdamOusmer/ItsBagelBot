// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"

	"ItsBagelBot/app/deployer/internal/ports"
)

func TestKVErrMapsOntoPortsSentinels(t *testing.T) {
	other := errors.New("nats: timeout")
	// A lost Create on the R3 hub bucket: nats.go wraps the 10164 API error
	// with ErrKeyExists instead of matching it by code.
	replicatedCreate := fmt.Errorf("%w: %w",
		&jetstream.APIError{ErrorCode: jetstream.JSErrCodeStreamWrongLastSequenceConstant}, jetstream.ErrKeyExists)
	cases := map[string]struct {
		in   error
		want error
	}{
		"nil":                  {nil, nil},
		"key not found":        {jetstream.ErrKeyNotFound, ports.ErrNotFound},
		"key exists":           {jetstream.ErrKeyExists, ports.ErrConflict},
		"replicated create":    {replicatedCreate, ports.ErrConflict},
		"revision mismatch":    {jetstream.ErrKeyRevisionMismatch, ports.ErrConflict},
		"invalid key":          {jetstream.ErrInvalidKey, ports.ErrInvalid},
		"anything else passes": {other, other},
	}
	for name, tc := range cases {
		assert.ErrorIs(t, kvErr(tc.in), tc.want, name)
	}
}

func TestLoadReportsCorruptValue(t *testing.T) {
	kv := &fakeKV{data: map[kvKey]record{}}
	kv.write(keyIndex, []byte("{not json"))
	_, _, found, err := load[[]string](context.Background(), kv, keyIndex)
	assert.Equal(t, [2]any{false, true}, [2]any{found, err != nil})
}
