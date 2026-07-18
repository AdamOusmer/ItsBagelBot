package projection

import (
	"testing"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
)

func TestHydrationStateComplete(t *testing.T) {
	require.False(t, (HydrationState{User: true, Modules: true}).Complete())
	require.True(t, (HydrationState{User: true, Modules: true, Commands: true}).Complete())
}

// retireStaleAliases is the read half of a read-modify-write over the row
// SetCommand overwrites. Read from a lagging node-local replica it computes the
// HDEL from an empty or older alias list, leaving retired aliases resolvable
// forever with nothing to revisit the row, so that read is pinned while the
// rest of the Store keeps node-local reads.
func TestNewStorePinsTheAliasRetirementRead(t *testing.T) {
	store := NewStore(nil)
	require.True(t, pkg_valkey.IsPrimary(store.primary))
	require.False(t, pkg_valkey.IsPrimary(store.client), "ordinary projection reads stay node-local")
}
