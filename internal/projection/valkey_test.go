package projection

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHydrationStateComplete(t *testing.T) {
	require.False(t, (HydrationState{User: true, Modules: true}).Complete())
	require.True(t, (HydrationState{User: true, Modules: true, Commands: true}).Complete())
}
