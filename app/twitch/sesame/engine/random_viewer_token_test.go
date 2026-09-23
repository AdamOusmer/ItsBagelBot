// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// stubViewerLookup answers a canned {random.viewer} state and counts reads,
// so the pipeline wiring is asserted end to end rather than through the
// scope package's fakes alone.
type stubViewerLookup struct {
	entries []chattersSnapshotEntry
	state   viewerSnapshotState
	calls   int
}

func (s *stubViewerLookup) Snapshot(context.Context, uint64) ([]chattersSnapshotEntry, viewerSnapshotState) {
	s.calls++
	return s.entries, s.state
}

func viewerEntry(id uint64, login string) chattersSnapshotEntry {
	return chattersSnapshotEntry{ID: id, Login: login, Name: login}
}

// randomViewerPipeline wires a channel for the {random.viewer} token: viewers
// answers the cached chat-list read, and speakers seed the roster the way a
// chat line would, for the MissingScope degrade path.
func randomViewerPipeline(t *testing.T, response string, viewers ViewerLookup, speakers ...chatterIdentity) *Pipeline {
	t.Helper()
	d := Deps{
		Proj: fakeReader{
			cmd:      projection.Command{Name: "brag", Response: response, IsActive: true, Perm: "everyone"},
			cmdFound: true,
			modules:  map[string]projection.ModuleView{},
		},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &fakePublisher{},
		Viewers:  viewers,
		Log:      zap.NewNop(),
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium:  premiumSubj,
		OutgressStandard: standardSubj,
		BotID:            chatterBotID,
	})
	for _, who := range speakers {
		p.roster.Observe(123, who)
	}
	return p
}

// The draw names a viewer, and names neither the bot, the broadcaster, nor
// the sender — {random.viewer}'s own exclusion, on top of the two
// {random.chatter} already applies (chatCtx's sender is id 999).
func TestRandomViewerTokenExcludesSenderBotAndBroadcaster(t *testing.T) {
	viewers := &stubViewerLookup{state: viewerSnapshotOK, entries: []chattersSnapshotEntry{
		viewerEntry(999, "alice"),    // the sender
		viewerEntry(555, "bagelbot"), // chatterBotID
		viewerEntry(123, "streamer"), // the broadcaster
		viewerEntry(42, "lurker"),
	}}
	p := randomViewerPipeline(t, "say hi to {random.viewer}", viewers)

	assert.Equal(t, "say hi to lurker", expandViewer(t, p, "!brag"))
	assert.Equal(t, 1, viewers.calls)
}

// A cold snapshot (no cache yet, a fetch may now be in flight) degrades to
// the roster rather than going empty: a hole in the reply is worse than
// naming someone who spoke recently instead of someone merely present. The
// draw never waits on the fetch. The sender exclusion still applies.
func TestRandomViewerTokenDegradesToTheRosterOnAColdSnapshot(t *testing.T) {
	viewers := &stubViewerLookup{state: viewerSnapshotCold}
	p := randomViewerPipeline(t, "say hi to {random.viewer}", viewers,
		spoke("999", "alice", "Alice"), spoke("42", "sam", "Sam"))

	assert.Equal(t, "say hi to Sam", expandViewer(t, p, "!brag"))
}

// An empty roster is the one case a cold snapshot still renders empty: there
// is genuinely nobody to name either way.
func TestRandomViewerTokenRendersEmptyWhenColdAndTheRoomIsEmpty(t *testing.T) {
	viewers := &stubViewerLookup{state: viewerSnapshotCold}
	p := randomViewerPipeline(t, "hi {random.viewer|someone}", viewers)
	assert.Equal(t, "hi someone", expandViewer(t, p, "!brag"))
}

// A channel latched MissingScope degrades to the very roster
// {random.chatter} draws from, rather than going silent for the rest of the
// stream. The sender exclusion still applies on this path too.
func TestRandomViewerTokenDegradesToTheRosterOnMissingScope(t *testing.T) {
	viewers := &stubViewerLookup{state: viewerSnapshotMissingScope}
	p := randomViewerPipeline(t, "say hi to {random.viewer}", viewers,
		spoke("999", "alice", "Alice"), spoke("42", "sam", "Sam"))

	assert.Equal(t, "say hi to Sam", expandViewer(t, p, "!brag"))
}

// Without Viewers wired the span renders empty (the family mounts
// unconditionally), matching an unnamed roster's answer for
// {random.chatter}.
func TestRandomViewerTokenRendersEmptyWithoutTheDependency(t *testing.T) {
	p := randomViewerPipeline(t, "hi {random.viewer|nobody}", nil)
	assert.Equal(t, "hi nobody", expandViewer(t, p, "!brag"))
}
