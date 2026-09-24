package rpc

import (
	"ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCounterSetDecimalValue(t *testing.T) {
	value, err := counterSetValue(loyaltyrpc.Request{CounterValue: "9223372036854775807", Value: 1})
	require.NoError(t, err)
	require.Equal(t, int64(9223372036854775807), value)
	value, err = counterSetValue(loyaltyrpc.Request{Value: 42})
	require.NoError(t, err)
	require.Equal(t, int64(42), value)
	for _, input := range []string{"-1", "9223372036854775808", "1.5", "NaN"} {
		_, err = counterSetValue(loyaltyrpc.Request{CounterValue: input})
		require.ErrorIs(t, err, repository.ErrInvalidInput)
	}
}
