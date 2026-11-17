// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/internal/valkeytest"
	"github.com/stretchr/testify/require"
)

func TestProcessTwoPodsRetainReporterOwnership(t *testing.T) {
	for _, dialect := range []string{"sqlite3", "mysql"} {
		for _, failure := range []bool{false, true} {
			name := dialect + "/upstream-copy"
			if failure {
				name = dialect + "/transient"
			}
			t.Run(name, func(t *testing.T) { testReporterOwnership(t, dialect, failure) })
		}
	}
}

func testReporterOwnership(t *testing.T, dialect string, failure bool) {
	verifier := newCompletionReceipts(t, dialect)
	b := newCompletionBroker(t)
	client := valkeytest.Spawn(t)
	p, reporter := b.pipeline(t, client)
	t.Cleanup(reporter.Close)
	t.Cleanup(p.Close)
	peer, peerReporter := b.pipeline(t, client)
	t.Cleanup(peerReporter.Close)
	t.Cleanup(peer.Close)
	b.pub.fail.Store(failure)
	b.publish(t)
	first := b.receive(t)
	require.NoError(t, p.Process(first))
	assertPendingCompletion(t, first)
	if failure {
		p.flushReporters(t.Context())
	}
	b.publish(t)
	copy := b.receive(t)
	if err := peer.Process(copy); err != nil {
		require.ErrorAs(t, err, new(effectBusy))
	}
	awaitCompletion(t, copy, false)
	b.pub.fail.Store(false)
	p.Finish(t.Context())
	awaitCompletion(t, first, true)
	retry := b.receive(t)
	require.NoError(t, peer.Process(retry))
	peer.Finish(t.Context())
	awaitCompletion(t, retry, true)
	verifier.replay(t, b.js)
}
