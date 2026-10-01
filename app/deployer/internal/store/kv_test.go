// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"

	"ItsBagelBot/app/deployer/internal/ports"
)

func TestKVErrMapsOntoPortsSentinels(t *testing.T) {
	other := errors.New("nats: timeout")
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
