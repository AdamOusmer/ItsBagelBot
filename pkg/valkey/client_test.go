package valkey

import (
	"crypto/tls"
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
