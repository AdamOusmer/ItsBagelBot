// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"crypto/tls"
	"log"
	"net"
	"os"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

const (
	writePoolSize       = 64
	writePoolMinSize    = 4
	writePoolBufferSize = 32 << 10
	writePoolIdleTime   = 30 * time.Second

	localPipelineMultiplex = 5
	localBufferSize        = 8 << 10
)

func BuildClientOption(address, password string) valkey_go.ClientOption {
	return withWritePool(buildOption(address, password, nil))
}

func withWritePool(opts valkey_go.ClientOption) valkey_go.ClientOption {
	opts.DisableAutoPipelining = true
	opts.BlockingPoolSize = writePoolSize
	opts.BlockingPoolCleanup = writePoolIdleTime
	opts.BlockingPoolMinSize = writePoolMinSize
	opts.ReadBufferEachConn = writePoolBufferSize
	opts.WriteBufferEachConn = writePoolBufferSize
	return opts
}

// No replica reads: this route serves writes, Primary views and keyspace subscriptions.
func buildOption(address, password string, tlsConfig *tls.Config) valkey_go.ClientOption {
	opts := valkey_go.ClientOption{
		InitAddress:  []string{address},
		Password:     password,
		DisableCache: true,
		TLSConfig:    cloneTLSConfig(tlsConfig),
	}
	if tlsConfig != nil {
		opts.DialCtxFn = nativeTLSDial
	}

	if isSentinelAddress(address) {
		opts.Sentinel = valkey_go.SentinelOption{
			MasterSet: "myprimary",
			Password:  password,
			TLSConfig: cloneTLSConfig(tlsConfig),
		}
	}
	return opts
}

type Client struct {
	valkey_go.Client
	local  valkey_go.Client
	pubsub *lazyValkeyClient
}

func NewClient(address, password string) (valkey_go.Client, error) {
	tlsConfig, err := clientTLSConfig()
	if err != nil {
		return nil, err
	}
	address = secureAddress(address, tlsConfig != nil)
	master, err := valkey_go.NewClient(withWritePool(buildOption(address, password, tlsConfig)))
	if err != nil {
		return nil, err
	}

	if !isSentinelAddress(address) {
		return &Client{Client: master}, nil
	}

	wrapped := &Client{
		Client: master,
		pubsub: newLazyValkeyClient(
			buildOption(address, password, tlsConfig),
			valkey_go.NewClient,
		),
	}

	nodeIP := os.Getenv("NODE_IP")
	if nodeIP == "" {
		return wrapped, nil
	}

	localAddress := (localEndpoint{
		nodeIP:     nodeIP,
		configured: os.Getenv("VALKEY_LOCAL_ADDR"),
		tlsEnabled: tlsConfig != nil,
	}).address()
	local, err := valkey_go.NewClient((localReadConfig{
		address:   localAddress,
		password:  password,
		tlsConfig: tlsConfig,
	}).option())
	if err != nil {
		log.Printf("valkey: node-local read client unavailable (%v); reading via Sentinel", err)
		return wrapped, nil
	}

	log.Printf("valkey: reading from node-local instance %s", localAddress)
	wrapped.local = local
	return wrapped, nil
}

type localReadConfig struct {
	address   string
	password  string
	tlsConfig *tls.Config
}

func (c localReadConfig) option() valkey_go.ClientOption {
	return valkey_go.ClientOption{
		InitAddress:         []string{c.address},
		Password:            c.password,
		DisableCache:        true,
		TLSConfig:           cloneTLSConfig(c.tlsConfig),
		PipelineMultiplex:   localPipelineMultiplex,
		ReadBufferEachConn:  localBufferSize,
		WriteBufferEachConn: localBufferSize,
	}
}

type localEndpoint struct {
	nodeIP     string
	configured string
	tlsEnabled bool
}

func (e localEndpoint) address() string {
	if e.configured != "" {
		return secureAddress(e.configured, e.tlsEnabled)
	}
	port := plainDataPort
	if e.tlsEnabled {
		port = tlsDataPort
	}
	return net.JoinHostPort(e.nodeIP, port)
}
