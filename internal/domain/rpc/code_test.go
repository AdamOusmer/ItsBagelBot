// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"

	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errSentinel = errors.New("bound elsewhere")

type oldReply struct {
	Value string `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

type newReply struct {
	Value string `json:"value,omitempty"`
	rpc.Refusal
}

func TestFailClassifies(t *testing.T) {
	rules := []rpc.Rule{
		rpc.Is(errSentinel, rpc.CodeConflict),
		rpc.When(func(err error) bool { return err.Error() == "gone" }, rpc.CodeNotFound),
	}
	cases := []struct {
		name string
		err  error
		want rpc.Refusal
	}{
		{"nil is not a refusal", nil, rpc.Refusal{}},
		{"a wrapped sentinel maps to its code", fmt.Errorf("save: %w", errSentinel), rpc.Refusal{Error: "save: bound elsewhere", Code: rpc.CodeConflict}},
		{"a predicate rule maps to its code", errors.New("gone"), rpc.Refusal{Error: "gone", Code: rpc.CodeNotFound}},
		{"a deadline is unavailable", fmt.Errorf("query: %w", context.DeadlineExceeded), rpc.Refusal{Error: "query: context deadline exceeded", Code: rpc.CodeUnavailable}},
		{"a cancellation is unavailable", fmt.Errorf("query: %w", context.Canceled), rpc.Refusal{Error: "query: context canceled", Code: rpc.CodeUnavailable}},
		{"anything else is internal", errors.New("boom"), rpc.Refusal{Error: "boom", Code: rpc.CodeInternal}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, rpc.Fail(tc.err, rules...))
		})
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return false }

func TestFailClassifiesConnectionErrors(t *testing.T) {
	eofRule := []rpc.Rule{rpc.Is(io.ErrUnexpectedEOF, rpc.CodeConflict)}
	cases := []struct {
		name  string
		err   error
		rules []rpc.Rule
		want  rpc.Code
	}{
		{"a bad conn is unavailable", driver.ErrBadConn, nil, rpc.CodeUnavailable},
		{"a wrapped bad conn is unavailable", fmt.Errorf("query: %w", driver.ErrBadConn), nil, rpc.CodeUnavailable},
		{"an unexpected eof is unavailable", fmt.Errorf("read: %w", io.ErrUnexpectedEOF), nil, rpc.CodeUnavailable},
		{"a connection reset is unavailable", fmt.Errorf("read: %w", syscall.ECONNRESET), nil, rpc.CodeUnavailable},
		{"a broken pipe is unavailable", fmt.Errorf("write: %w", syscall.EPIPE), nil, rpc.CodeUnavailable},
		{"a net op error is unavailable", fmt.Errorf("dial: %w", &net.OpError{Op: "dial", Err: errors.New("refused")}), nil, rpc.CodeUnavailable},
		{"a net timeout is unavailable", fmt.Errorf("wait: %w", timeoutError{}), nil, rpc.CodeUnavailable},
		{"a plain error is internal", errors.New("boom"), nil, rpc.CodeInternal},
		{"a plain eof is internal", io.EOF, nil, rpc.CodeInternal},
		{"an explicit rule wins over the network default", fmt.Errorf("read: %w", io.ErrUnexpectedEOF), eofRule, rpc.CodeConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, rpc.Fail(tc.err, tc.rules...).Code)
		})
	}
}

func TestWireContract(t *testing.T) {
	assert.Equal(t, []rpc.Code{"invalid", "not_found", "forbidden", "conflict", "unavailable", "internal"}, rpc.Codes())

	t.Run("old and new readers understand each other", func(t *testing.T) {
		fromNew, err := codec.MarshalToString(newReply{Refusal: rpc.Refused(rpc.CodeNotFound, "no such user")})
		require.NoError(t, err)
		var old oldReply
		require.NoError(t, codec.UnmarshalFromString(fromNew, &old))
		assert.Equal(t, "no such user", old.Error)

		fromOld, err := codec.MarshalToString(oldReply{Error: "no such user"})
		require.NoError(t, err)
		var fresh newReply
		require.NoError(t, codec.UnmarshalFromString(fromOld, &fresh))
		assert.Equal(t, rpc.Refusal{Error: "no such user", Code: rpc.CodeOK}, fresh.Refusal)
	})

	t.Run("success omits the refusal fields", func(t *testing.T) {
		got, err := codec.MarshalToString(newReply{Value: "ok"})
		require.NoError(t, err)
		assert.Equal(t, `{"value":"ok"}`, got)
	})
}
