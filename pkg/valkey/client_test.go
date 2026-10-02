// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

func TestBuildClientOptionSelectsSentinelByPort(t *testing.T) {
	for _, tc := range []struct {
		name    string
		address string
		want    valkey_go.SentinelOption
	}{
		{"a data address stays standalone", "valkey:6379", valkey_go.SentinelOption{}},
		{
			"a sentinel address names the primary set",
			"valkey.svc.cluster.local:26379",
			valkey_go.SentinelOption{MasterSet: "myprimary", Password: "password"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := BuildClientOption(tc.address, "password")

			assert.Equal(t, tc.want, opts.Sentinel)
			assert.Nil(t, opts.SendToReplicas, "the primary route must remain primary-consistent")
		})
	}
}

func TestPubSubClientIsLazyAndSingleFlight(t *testing.T) {
	primary := &recordingValkeyClient{}
	pubsub := &recordingValkeyClient{}
	var creates atomic.Int64
	client := &Client{
		Client: primary,
		pubsub: newLazyValkeyClient(valkey_go.ClientOption{}, func(valkey_go.ClientOption) (valkey_go.Client, error) {
			creates.Add(1)
			return pubsub, nil
		}),
	}
	assert.Zero(t, creates.Load())

	const receivers = 32
	var group sync.WaitGroup
	group.Add(receivers)
	for range receivers {
		go func() {
			defer group.Done()
			assert.NoError(t, client.Receive(context.Background(), valkey_go.Completed{}, nil))
		}()
	}
	group.Wait()
	client.Close()

	assert.Equal(t, int64(1), creates.Load())
	assert.Equal(t, int64(receivers), pubsub.receives.Load())
	assert.Equal(t, int64(1), pubsub.closes.Load())
	assert.Equal(t, int64(1), primary.closes.Load())
}

func TestClosedClientCannotCreateLazyPubSubClient(t *testing.T) {
	var creates atomic.Int64
	client := &Client{
		Client: &recordingValkeyClient{},
		pubsub: newLazyValkeyClient(valkey_go.ClientOption{}, func(valkey_go.ClientOption) (valkey_go.Client, error) {
			creates.Add(1)
			return &recordingValkeyClient{}, nil
		}),
	}

	client.Close()
	err := client.Receive(context.Background(), valkey_go.Completed{}, nil)

	assert.ErrorIs(t, err, valkey_go.ErrClosing)
	assert.Zero(t, creates.Load())
}

func TestOptionalClientFailsPromptlyWhileDisconnected(t *testing.T) {
	c := newOptionalClient(func() (valkey_go.Client, error) { return nil, errOptionalUnavailable })
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	single := c.Do(ctx, c.B().Get().Key("test").Build()).Error()
	results := c.DoMulti(ctx, c.B().Get().Key("one").Build(), c.B().Get().Key("two").Build())

	require.ErrorIs(t, single, errOptionalUnavailable)
	require.Len(t, results, 2)
	require.ErrorIs(t, results[0].Error(), errOptionalUnavailable)
}

func TestOptionalClientPublishesConnectionAndClosesIt(t *testing.T) {
	ready := &optionalReady{}
	c := newOptionalClient(func() (valkey_go.Client, error) { return ready, nil })
	require.Eventually(t, func() bool { return c.current() == ready }, time.Second, time.Millisecond)

	c.Close()
	c.Close()

	require.True(t, ready.closed.Load())
}

func TestNewClientTalksToTheServerOverPlainAndMutualTLS(t *testing.T) {
	for _, tc := range []struct {
		name string
		tls  bool
	}{
		{"a plain connection", false},
		{"a mutual TLS connection", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			if tc.tls {
				pki := newTestPKI(t)
				files := newClientCertFiles(t)
				pki.writeClientCert(t, files, 1)
				t.Setenv("VALKEY_TLS_CA_PEM", string(pki.caPEM()))
				t.Setenv("VALKEY_TLS_SERVER_NAME", "valkey-test.local")
				t.Setenv("VALKEY_TLS_CLIENT_CERT_FILE", files.cert)
				t.Setenv("VALKEY_TLS_CLIENT_KEY_FILE", files.key)
				ln = tls.NewListener(ln, pki.serverConfig(t))
			} else {
				t.Setenv("VALKEY_TLS_CA_PEM", "")
			}
			server := startFakeValkey(t, ln)

			client, err := NewClient(ln.Addr().String(), "")
			require.NoError(t, err)
			defer client.Close()
			ctx := context.Background()
			require.NoError(t, client.Do(ctx, client.B().Set().Key("k").Value("v").Build()).Error())
			got, err := client.Do(ctx, client.B().Get().Key("k").Build()).ToString()
			_, missing := client.Do(ctx, client.B().Get().Key("absent").Build()).ToString()
			batch := client.DoMulti(ctx, client.B().Set().Key("b").Value("w").Build(), client.B().Get().Key("b").Build())

			require.NoError(t, err)
			assert.Equal(t, "v", got)
			assert.True(t, valkey_go.IsValkeyNil(missing), "a missing key reads as nil")
			require.Len(t, batch, 2)
			batched, batchErr := batch[1].ToString()
			require.NoError(t, batchErr)
			assert.Equal(t, "w", batched)
			stored, ok := server.value("k")
			assert.True(t, ok)
			assert.Equal(t, "v", stored)
		})
	}
}
