package valkey

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	valkey_go "github.com/valkey-io/valkey-go"
)

// TestPrimaryOption verifies that the authoritative client never delegates
// reads to a random Sentinel replica. Node-local routing belongs to Client.
func TestPrimaryOption(t *testing.T) {
	t.Run("Standard Address", func(t *testing.T) {
		opts := BuildClientOption("valkey:6379", "password")
		assert.Nil(t, opts.SendToReplicas, "SendToReplicas should be nil for standard connections")
		assertWritePool(t, opts)
	})

	t.Run("Sentinel Address", func(t *testing.T) {
		opts := BuildClientOption("valkey.svc.cluster.local:26379", "password")
		assert.Nil(t, opts.SendToReplicas, "the primary route must remain primary-consistent")

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
	opts := buildOption("valkey.valkey.svc.cluster.local:26380", "password", config)

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
		(localEndpoint{nodeIP: "100.95.95.9", configured: "valkey-local.valkey.svc.cluster.local:6379", tlsEnabled: true}).address(),
	)
	assert.Equal(t, "100.95.95.9:6380", (localEndpoint{nodeIP: "100.95.95.9", tlsEnabled: true}).address())
	assert.Equal(t, "100.95.95.9:6379", (localEndpoint{nodeIP: "100.95.95.9"}).address())
}

func TestLocalReadOptionUsesSmallMultiplexedConnections(t *testing.T) {
	opts := (localReadConfig{
		address:   "valkey-local:6380",
		password:  "password",
		tlsConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}).option()
	assert.Equal(t, []string{"valkey-local:6380"}, opts.InitAddress)
	assert.False(t, opts.DisableAutoPipelining)
	assert.Equal(t, localPipelineMultiplex, opts.PipelineMultiplex)
	assert.Equal(t, localBufferSize, opts.ReadBufferEachConn)
	assert.Equal(t, localBufferSize, opts.WriteBufferEachConn)
	assert.NotNil(t, opts.TLSConfig)
}

func TestNativeDialTargetUsesLocalServiceForElectedPrimary(t *testing.T) {
	target := nativeDialTarget{
		discovered:   "100.95.95.9:6379",
		nodeIP:       "100.95.95.9",
		localAddress: "valkey-local.valkey.svc.cluster.local:6380",
	}
	assert.Equal(t, "valkey-local.valkey.svc.cluster.local:6380", target.address())
}

func TestNativeDialTargetPreservesRemoteAndSentinelAddresses(t *testing.T) {
	remote := nativeDialTarget{
		discovered:   "100.99.41.21:6379",
		nodeIP:       "100.95.95.9",
		localAddress: "valkey-local.valkey.svc.cluster.local:6380",
	}
	assert.Equal(t, "100.99.41.21:6380", remote.address())

	sentinel := nativeDialTarget{
		discovered:   "valkey.valkey.svc.cluster.local:26379",
		nodeIP:       "100.95.95.9",
		localAddress: "valkey-local.valkey.svc.cluster.local:6380",
	}
	assert.Equal(t, "valkey.valkey.svc.cluster.local:26380", sentinel.address())
}
