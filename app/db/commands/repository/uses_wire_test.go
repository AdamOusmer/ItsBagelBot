package repository_test

import (
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/codec"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCommandUsesWirePreservesInt64Digits(t *testing.T) {
	changed := data.CommandChangedDTO{Uses: data.MaxCounter}
	body, err := codec.FastMarshal(changed)
	require.NoError(t, err)
	require.Contains(t, string(body), `"uses":"9223372036854775807"`)
	var decoded data.CommandChangedDTO
	require.NoError(t, codec.Unmarshal(body, &decoded))
	require.Equal(t, data.MaxCounter, decoded.Uses)
	view := projection.CommandView{Uses: data.MaxCounter}
	body, err = codec.FastMarshal(view)
	require.NoError(t, err)
	require.Contains(t, string(body), `"uses":"9223372036854775807"`)
}

func TestCommandUsesAcceptsHistoricalNumberAndNewString(t *testing.T) {
	for _, body := range []string{`{"uses":9223372036854775807}`, `{"uses":"9223372036854775807"}`} {
		var changed data.CommandChangedDTO
		require.NoError(t, codec.Unmarshal([]byte(body), &changed))
		require.Equal(t, data.MaxCounter, changed.Uses)
		var view projection.CommandView
		require.NoError(t, codec.Unmarshal([]byte(body), &view))
		require.Equal(t, data.MaxCounter, view.Uses)
	}
	for _, body := range []string{`{"uses":9223372036854775808}`, `{"uses":"9223372036854775808"}`, `{"uses":1.1}`} {
		var view projection.CommandView
		require.Error(t, codec.Unmarshal([]byte(body), &view))
	}
}
