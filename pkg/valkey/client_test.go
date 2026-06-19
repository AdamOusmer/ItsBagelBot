package valkey

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestReadScaling verifies that the Valkey client option configuration correctly
// sets the SendToReplicas predicate when connecting to a Sentinel cluster.
// This is critical to ensure that local read-only queries are offloaded to local replicas!
func TestReadScaling_IsReadOnly(t *testing.T) {
	t.Run("Standard Address", func(t *testing.T) {
		opts := BuildClientOption("valkey:6379", "password")
		assert.Nil(t, opts.SendToReplicas, "SendToReplicas should be nil for standard connections")
	})

	t.Run("Sentinel Address", func(t *testing.T) {
		opts := BuildClientOption("valkey.svc.cluster.local:26379", "password")
		assert.NotNil(t, opts.SendToReplicas, "SendToReplicas must be configured for Sentinel to enforce local preferred reads")

		// We can't easily construct a valkey_go.Completed without a real client, 
		// but we can assert the option was configured and the master set logic applied.
		assert.Equal(t, "myprimary", opts.Sentinel.MasterSet)
		assert.Equal(t, "password", opts.Sentinel.Password)
	})
}
