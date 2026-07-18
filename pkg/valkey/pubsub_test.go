package valkey

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	valkey_go "github.com/valkey-io/valkey-go"
)

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

	assert.Equal(t, int64(1), creates.Load())
	assert.Equal(t, int64(receivers), pubsub.receives.Load())
	client.Close()
	assert.Equal(t, int64(1), pubsub.closes.Load())
	assert.Equal(t, int64(1), primary.closes.Load())
}

func TestClosedClientCannotCreateLazyPubSubClient(t *testing.T) {
	primary := &recordingValkeyClient{}
	var creates atomic.Int64
	client := &Client{
		Client: primary,
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
