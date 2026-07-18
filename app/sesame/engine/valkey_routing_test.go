package engine

import (
	"testing"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/assert"
)

// Reads default to the node-local replica, which lags the primary. These stores
// read back state their own caller just wrote, so their reads are pinned; a
// refactor that drops a Primary wrap fails here instead of silently serving
// chat a value that contradicts the write that produced it.

func TestLoyaltyCounterViewIsPrimaryAndTheBalanceCacheIsNot(t *testing.T) {
	store := NewValkeyLoyaltyStore(nil, nil, nil, nil)

	assert.True(t, pkg_valkey.IsPrimary(store.primary),
		"a counter peek must agree with the master-run script that bumped it, and must observe CounterInvalidate's delete")
	assert.False(t, pkg_valkey.IsPrimary(store.client),
		"the balance cache keeps node-local reads: its staleness budget is balanceTTL, which dwarfs replication lag")
}

func TestQueueStoreReadsArePrimary(t *testing.T) {
	store := NewValkeyQueueStore(nil, 0, nil)

	assert.True(t, pkg_valkey.IsPrimary(store.client),
		"IsOpen gates joins on the flag SetOpen wrote and List renders the line Join just changed")
}
