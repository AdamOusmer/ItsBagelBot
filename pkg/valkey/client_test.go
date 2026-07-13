package valkey

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	valkey_go "github.com/valkey-io/valkey-go"
)

// TestReadScaling verifies that the Valkey client option configuration correctly
// sets the SendToReplicas predicate when connecting to a Sentinel cluster.
// This is critical to ensure that local read-only queries are offloaded to local replicas!
func TestReadScaling_IsReadOnly(t *testing.T) {
	t.Run("Standard Address", func(t *testing.T) {
		opts := BuildClientOption("valkey:6379", "password")
		assert.Nil(t, opts.SendToReplicas, "SendToReplicas should be nil for standard connections")
		assertWritePool(t, opts)
	})

	t.Run("Sentinel Address", func(t *testing.T) {
		opts := BuildClientOption("valkey.svc.cluster.local:26379", "password")
		assert.NotNil(t, opts.SendToReplicas, "SendToReplicas must be configured for Sentinel to enforce local preferred reads")

		// We can't easily construct a valkey_go.Completed without a real client,
		// but we can assert the option was configured and the master set logic applied.
		assert.Equal(t, "myprimary", opts.Sentinel.MasterSet)
		assert.Equal(t, "password", opts.Sentinel.Password)
		assertWritePool(t, opts)
	})
}

func assertWritePool(t *testing.T, opts valkey_go.ClientOption) {
	t.Helper()
	assert.True(t, opts.DisableAutoPipelining)
	assert.Equal(t, writePoolSize, opts.BlockingPoolSize)
	assert.Equal(t, writePoolMinSize, opts.BlockingPoolMinSize)
	assert.Equal(t, writePoolIdleTime, opts.BlockingPoolCleanup)
	assert.Equal(t, writePoolBufferSize, opts.ReadBufferEachConn)
	assert.Equal(t, writePoolBufferSize, opts.WriteBufferEachConn)
}

func TestTLSOptionSecuresSentinelAndDataConnections(t *testing.T) {
	config := &tls.Config{ServerName: defaultTLSServerName, MinVersion: tls.VersionTLS12}
	opts := buildOption("valkey.valkey.svc.cluster.local:26380", "password", true, config)

	assert.NotNil(t, opts.TLSConfig)
	assert.NotSame(t, config, opts.TLSConfig)
	assert.Equal(t, defaultTLSServerName, opts.TLSConfig.ServerName)
	assert.NotNil(t, opts.Sentinel.TLSConfig)
	assert.NotSame(t, opts.TLSConfig, opts.Sentinel.TLSConfig)
	assert.NotNil(t, opts.DialCtxFn)
}

func TestSecureAddressUsesNativeTLSPorts(t *testing.T) {
	assert.Equal(t, "valkey.valkey.svc.cluster.local:6380", secureAddress("valkey.valkey.svc.cluster.local:6379", true))
	assert.Equal(t, "valkey.valkey.svc.cluster.local:26380", secureAddress("valkey.valkey.svc.cluster.local:26379", true))
	assert.Equal(t, "valkey.valkey.svc.cluster.local:26379", secureAddress("valkey.valkey.svc.cluster.local:26379", false))
}

func TestLocalReadAddressPrefersLocalService(t *testing.T) {
	assert.Equal(t,
		"valkey-local.valkey.svc.cluster.local:6380",
		localReadAddress("100.95.95.9", "valkey-local.valkey.svc.cluster.local:6379", true),
	)
	assert.Equal(t, "100.95.95.9:6380", localReadAddress("100.95.95.9", "", true))
	assert.Equal(t, "100.95.95.9:6379", localReadAddress("100.95.95.9", "", false))
}

func TestLocalReadOptionUsesSmallMultiplexedConnections(t *testing.T) {
	opts := localReadOption("valkey-local:6380", "password", &tls.Config{MinVersion: tls.VersionTLS12})
	assert.Equal(t, []string{"valkey-local:6380"}, opts.InitAddress)
	assert.False(t, opts.DisableAutoPipelining)
	assert.Equal(t, localPipelineMultiplex, opts.PipelineMultiplex)
	assert.Equal(t, localBufferSize, opts.ReadBufferEachConn)
	assert.Equal(t, localBufferSize, opts.WriteBufferEachConn)
	assert.NotNil(t, opts.TLSConfig)
}
