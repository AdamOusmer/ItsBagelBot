// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey_test

import (
	"testing"

	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
)

func TestDistinctUnionCountsOverlappingMembersWithoutChangingSources(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys pkgvalkey.DistinctUnion
		want int64
	}{
		{"mixed", pkgvalkey.DistinctUnion{Sketch: "sketch", Set: "set"}, 3},
		{"sketch", pkgvalkey.DistinctUnion{Sketch: "sketch"}, 2},
		{"set", pkgvalkey.DistinctUnion{Set: "set"}, 2},
		{"empty set", pkgvalkey.DistinctUnion{Sketch: "sketch", Set: "missing"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := valkeytest.Spawn(t)
			ctx := t.Context()
			require.NoError(t, client.Do(ctx, client.B().Pfadd().Key("sketch").Element("a", "b").Build()).Error())
			require.NoError(t, client.Do(ctx, client.B().Sadd().Key("set").Member("b", "c").Build()).Error())
			count, err := tc.keys.Count(ctx, client)
			require.NoError(t, err)
			require.Equal(t, tc.want, count)

			keys, err := client.Do(ctx, client.B().Keys().Pattern("*").Build()).AsStrSlice()
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"sketch", "set"}, keys)
			legacy, err := pkgvalkey.HyperLogLogCount(ctx, client, "sketch")
			require.NoError(t, err)
			require.EqualValues(t, 2, legacy)
			members, err := pkgvalkey.SetMembers(ctx, client, "set")
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"b", "c"}, members)
		})
	}
}

func TestDistinctUnionPropagatesInvalidSourceTypes(t *testing.T) {
	for _, source := range []string{"sketch", "set"} {
		t.Run(source, func(t *testing.T) {
			client := valkeytest.Spawn(t)
			ctx := t.Context()
			require.NoError(t, client.Do(ctx, client.B().Set().Key(source).Value("invalid").Build()).Error())
			_, err := (pkgvalkey.DistinctUnion{Sketch: "sketch", Set: "set"}).Count(ctx, client)
			require.Error(t, err)
			keys, err := client.Do(ctx, client.B().Keys().Pattern("*:union:*").Build()).AsStrSlice()
			require.NoError(t, err)
			require.Empty(t, keys)
		})
	}
}
