package valkey

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	valkey_go "github.com/valkey-io/valkey-go"
)

func TestClientViewsSelectRoutingAndPipelineSingleCommands(t *testing.T) {
	master := &recordingValkeyClient{}
	local := &recordingValkeyClient{}
	client := &Client{Client: master, local: local}

	localRoute := client.commandRoute(true, false, singleCommand)
	assert.Same(t, local, localRoute.client)
	assert.Equal(t, "valkey.read", localRoute.operation)
	primaryRoute := client.commandRoute(true, true, singleCommand)
	assert.Same(t, master, primaryRoute.client)
	assert.Equal(t, "valkey.read", primaryRoute.operation)

	view := Primary(Throughput(client)).(*clientView)
	assert.Same(t, client, view.routed)
	assert.True(t, view.primary)
	assert.True(t, view.throughput)

	read := (valkey_go.Builder{}).Arbitrary("GET", "key").ReadOnly()
	view.Do(context.Background(), read)
	assert.Len(t, master.commands, 1)
	assert.True(t, master.commands[0].IsReadOnly())
	assert.False(t, master.commands[0].IsPipe(), "primary reads remain on the consistency and latency path")
	assert.Empty(t, local.commands)

	view.Do(context.Background(), valkey_go.Completed{})
	assert.Len(t, master.commands, 2)
	assert.True(t, master.commands[1].IsPipe())

	view.DoMulti(context.Background(), valkey_go.Completed{}, valkey_go.Completed{})
	assert.Len(t, master.batches, 1)
	assert.Len(t, master.batches[0], 2)
	assert.False(t, master.batches[0][0].IsPipe(), "DoMulti already batches and must not be retagged")
	assert.False(t, master.batches[0][1].IsPipe(), "DoMulti already batches and must not be retagged")

	view.Close()
	assert.Zero(t, master.closes.Load(), "a borrowed view must not close its source")
}

func TestThroughputViewDelegatesForeignClientWithoutAnotherPool(t *testing.T) {
	client := &recordingValkeyClient{}
	view := Throughput(client)

	view.Do(context.Background(), valkey_go.Completed{})
	assert.Len(t, client.commands, 1)
	assert.True(t, client.commands[0].IsPipe())
	view.Do(context.Background(), (valkey_go.Builder{}).Arbitrary("GET", "key").ReadOnly())
	assert.Len(t, client.commands, 2)
	assert.False(t, client.commands[1].IsPipe(), "read-only commands are not forced into throughput mode")

	view.DoMulti(context.Background(), valkey_go.Completed{})
	assert.Len(t, client.batches, 1)
	assert.False(t, client.batches[0][0].IsPipe())
}
