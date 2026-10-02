// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	fetchkeyrpc "ItsBagelBot/internal/domain/rpc/fetchkey"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestFetchViewWireTagsMatchProjectionContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		view fetchkeyrpc.FetchView
		want string
	}{
		{
			name: "every field present",
			view: fetchkeyrpc.FetchView{Name: "wx", URL: "https://x", JSONPath: []string{"a"}, KeyLabel: "k", IsActive: true},
			want: `{"name":"wx","url":"https://x","json_path":["a"],"key_label":"k","is_active":true}`,
		},
		{
			name: "omitted fields stay absent from the projected JSON",
			view: fetchkeyrpc.FetchView{Name: "plain", URL: "https://y"},
			want: `{"name":"plain","url":"https://y","is_active":false}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := codec.Marshal(tc.view)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(body))
		})
	}
}
