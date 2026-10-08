// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	valkey_go "github.com/valkey-io/valkey-go"
)

type routedCall struct {
	target string
	piped  bool
}

type routedPair struct {
	primary *recordingValkeyClient
	local   *recordingValkeyClient
	routed  *Client
}

func newRoutedPair() routedPair {
	pair := routedPair{primary: &recordingValkeyClient{}, local: &recordingValkeyClient{}}
	pair.routed = &Client{Client: pair.primary, local: pair.local}
	return pair
}

func (p routedPair) observed() []routedCall {
	var calls []routedCall
	for target, recorded := range map[string]*recordingValkeyClient{"primary": p.primary, "local": p.local} {
		for _, cmd := range recorded.commands {
			calls = append(calls, routedCall{target, cmd.IsPipe()})
		}
		for _, batch := range recorded.batches {
			for _, cmd := range batch {
				calls = append(calls, routedCall{target, cmd.IsPipe()})
			}
		}
	}
	return calls
}

func readCommand() valkey_go.Completed {
	return (valkey_go.Builder{}).Arbitrary("GET", "key").ReadOnly()
}

func TestViewsRouteReadsAndPipelineOnlyThroughputWrites(t *testing.T) {
	routed := func(p routedPair) valkey_go.Client { return p.routed }
	foreign := func(p routedPair) valkey_go.Client { return p.primary }
	read := func(c valkey_go.Client) { c.Do(context.Background(), readCommand()) }
	write := func(c valkey_go.Client) { c.Do(context.Background(), valkey_go.Completed{}) }
	batch := func(c valkey_go.Client) {
		c.DoMulti(context.Background(), valkey_go.Completed{}, valkey_go.Completed{})
	}

	for _, tc := range []struct {
		name string
		base func(routedPair) valkey_go.Client
		view func(valkey_go.Client) valkey_go.Client
		act  func(valkey_go.Client)
		want []routedCall
	}{
		{"a bare client reads from the node-local pool", routed, func(c valkey_go.Client) valkey_go.Client { return c }, read, []routedCall{{"local", false}}},
		{"a bare client writes to the primary unpiped", routed, func(c valkey_go.Client) valkey_go.Client { return c }, write, []routedCall{{"primary", false}}},
		{"a throughput view alone does not pin reads to the primary", routed, Throughput, read, []routedCall{{"local", false}}},
		{"a primary view keeps reads on the consistency and latency path", routed, Primary, read, []routedCall{{"primary", false}}},
		{"a throughput view pipelines writes", routed, Throughput, write, []routedCall{{"primary", true}}},
		{"a primary throughput view pipelines writes on the primary", routed, PrimaryThroughput, write, []routedCall{{"primary", true}}},
		{"a batch is never retagged because DoMulti already batches", routed, PrimaryThroughput, batch, []routedCall{{"primary", false}, {"primary", false}}},
		{"a throughput view over a foreign client pipelines writes", foreign, Throughput, write, []routedCall{{"primary", true}}},
		{"a throughput view over a foreign client leaves reads unpiped", foreign, Throughput, read, []routedCall{{"primary", false}}},
		{"a throughput view over a foreign client leaves batches unpiped", foreign, Throughput, batch, []routedCall{{"primary", false}, {"primary", false}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pair := newRoutedPair()

			tc.act(tc.view(tc.base(pair)))

			assert.Equal(t, tc.want, pair.observed())
		})
	}
}

func TestClosingAViewLeavesItsSourceOpen(t *testing.T) {
	source := &recordingValkeyClient{}

	Primary(Throughput(source)).Close()

	assert.Zero(t, source.closes.Load(), "a borrowed view must not close its source")
}

func TestIsPrimaryReportsTheReadRouteOfAView(t *testing.T) {
	client := &recordingValkeyClient{}

	for _, tc := range []struct {
		name string
		view valkey_go.Client
		want bool
	}{
		{"a bare client reads wherever its own policy sends it", client, false},
		{"throughput alone does not pin reads to the primary", Throughput(client), false},
		{"a primary view is primary", Primary(client), true},
		{"a primary throughput view is primary", PrimaryThroughput(client), true},
		{"wrapping a primary view keeps it primary", Throughput(Primary(client)), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsPrimary(tc.view))
		})
	}
}
