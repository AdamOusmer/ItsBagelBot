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

func TestRandomViewerTokenExcludesSenderBotAndBroadcaster(t *testing.T) {
	viewers := &stubViewerLookup{state: viewerSnapshotOK, entries: []chattersSnapshotEntry{
		viewerEntry(999, "alice"),
		viewerEntry(555, "bagelbot"),
		viewerEntry(123, "streamer"),
		viewerEntry(42, "lurker"),
	}}
	p := randomViewerPipeline(t, "say hi to {random.viewer}", viewers)

	assert.Equal(t, "say hi to lurker", expandViewer(t, p, "!brag"))
	assert.Equal(t, 1, viewers.calls)
}

func TestRandomViewerTokenDegradesToTheRosterOnAColdSnapshot(t *testing.T) {
	viewers := &stubViewerLookup{state: viewerSnapshotCold}
	p := randomViewerPipeline(t, "say hi to {random.viewer}", viewers,
		spoke("999", "alice", "Alice"), spoke("42", "sam", "Sam"))

	assert.Equal(t, "say hi to Sam", expandViewer(t, p, "!brag"))
}

func TestRandomViewerTokenRendersEmptyWhenColdAndTheRoomIsEmpty(t *testing.T) {
	viewers := &stubViewerLookup{state: viewerSnapshotCold}
	p := randomViewerPipeline(t, "hi {random.viewer|someone}", viewers)
	assert.Equal(t, "hi someone", expandViewer(t, p, "!brag"))
}

func TestRandomViewerTokenDegradesToTheRosterOnMissingScope(t *testing.T) {
	viewers := &stubViewerLookup{state: viewerSnapshotMissingScope}
	p := randomViewerPipeline(t, "say hi to {random.viewer}", viewers,
		spoke("999", "alice", "Alice"), spoke("42", "sam", "Sam"))

	assert.Equal(t, "say hi to Sam", expandViewer(t, p, "!brag"))
}

func TestRandomViewerTokenRendersEmptyWithoutTheDependency(t *testing.T) {
	p := randomViewerPipeline(t, "hi {random.viewer|nobody}", nil)
	assert.Equal(t, "hi nobody", expandViewer(t, p, "!brag"))
}
