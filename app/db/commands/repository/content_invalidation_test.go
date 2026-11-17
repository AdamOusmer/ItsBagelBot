// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type contentRead func(context.Context, uint64) (string, error)

type contentRig struct {
	readers []contentRead
	erase   func(context.Context, uint64, time.Time) error
	rename  func(context.Context) error
}

type contentKind bool

const (
	commandContent contentKind = false
	fetchContent   contentKind = true
)

func newContentRig(t *testing.T, kind contentKind, publishers []bus.Publisher) contentRig {
	t.Helper()
	client, _, unused := setup(t)
	t.Cleanup(func() { unused.Close(t.Context()) })
	if kind == fetchContent {
		return fetchContentRig(t, client, publishers)
	}
	client.Commands.Create().SetUserID(1001).SetName("old").SetResponse("hi").SetPerm("everyone").SaveX(t.Context())
	rig := contentRig{}
	for _, pub := range publishers {
		repo := repository.NewCommands(client, pub, nil, zap.NewNop())
		t.Cleanup(func() { repo.Close(t.Context()) })
		rig.readers = append(rig.readers, contentNames(repo.List, func(v repository.CommandView) string { return v.Name }))
		rig.erase = repo.DeleteAllForUser
		rig.rename = func(ctx context.Context) error { return repo.Rename(ctx, 1001, "old", spec("new", "hi", false, 0)) }
	}
	return rig
}

func fetchContentRig(t *testing.T, client *ent.Client, publishers []bus.Publisher) contentRig {
	t.Helper()
	client.FetchDefinition.Create().SetUserID(1001).SetName("old").SetURL("https://api.example.com").SaveX(t.Context())
	rig := contentRig{}
	for _, pub := range publishers {
		repo := repository.NewFetches(client, nil, pub, zap.NewNop())
		t.Cleanup(repo.Close)
		rig.readers = append(rig.readers, contentNames(repo.List, func(v repository.FetchView) string { return v.Name }))
		rig.erase = repo.DeleteAllForUser
		rig.rename = func(ctx context.Context) error {
			return repo.RenameDef(ctx, 1001, "old", fetchSpec("new", "https://api.example.com"))
		}
	}
	return rig
}

func contentNames[V any](list func(context.Context, uint64) ([]V, error), name func(V) string) contentRead {
	return func(ctx context.Context, id uint64) (string, error) {
		rows, err := list(ctx, id)
		if err != nil || len(rows) == 0 {
			return "", err
		}
		return name(rows[0]), nil
	}
}

func requireContentNames(t *testing.T, rig contentRig, want string) {
	t.Helper()
	for _, read := range rig.readers {
		name, err := read(t.Context(), 1001)
		require.NoError(t, err)
		require.Equal(t, want, name)
	}
}

var errSecondContentPublish = errors.New("second change publish failed")

type failingContentPublisher struct {
	bus.Publisher
	calls int
}

func (p *failingContentPublisher) PublishOwned(ctx context.Context, topic string, payload []byte) error {
	return p.PublishOwnedWithID(ctx, topic, "", payload)
}

func (p *failingContentPublisher) PublishOwnedWithID(ctx context.Context, topic, _ string, payload []byte) error {
	p.calls++
	if p.calls == 2 {
		return errSecondContentPublish
	}
	return p.Publisher.PublishOwned(ctx, topic, payload)
}

func TestContentMutationsEvictWriterAndReplicaAfterCommit(t *testing.T) {
	mutations := []struct {
		name  string
		apply func(context.Context, contentRig) error
		want  string
		err   error
	}{
		{name: "delete", apply: func(ctx context.Context, rig contentRig) error {
			return rig.erase(ctx, 1001, time.Now().Add(time.Hour))
		}},
		{name: "rename second publish failure", apply: func(ctx context.Context, rig contentRig) error { return rig.rename(ctx) }, want: "new", err: errSecondContentPublish},
	}
	for _, kind := range []contentKind{commandContent, fetchContent} {
		for _, mutation := range mutations {
			t.Run(map[contentKind]string{commandContent: "commands", fetchContent: "fetches"}[kind]+"/"+mutation.name, func(t *testing.T) {
				h := bus.NewInvalidations(nil, "", nil)
				pub := &failingContentPublisher{Publisher: bustest.NewPublisher()}
				bound := h.Publisher(pub)
				rig := newContentRig(t, kind, []bus.Publisher{bound, bound})
				requireContentNames(t, rig, "old")
				require.ErrorIs(t, mutation.apply(t.Context(), rig), mutation.err)
				requireContentNames(t, rig, mutation.want)
			})
		}
	}
}

func TestContentDeletionFansOutAcrossHubs(t *testing.T) {
	for _, kind := range []contentKind{commandContent, fetchContent} {
		t.Run(map[contentKind]string{commandContent: "commands", fetchContent: "fetches"}[kind], func(t *testing.T) {
			nc := bustest.NATS(t)
			var publishers []bus.Publisher
			for range 2 {
				h := bus.NewInvalidations(nc, "test.invalidate", nil)
				publishers = append(publishers, h.Publisher(bustest.NewPublisher()))
			}
			rig := newContentRig(t, kind, publishers)
			require.NoError(t, nc.Flush())
			requireContentNames(t, rig, "old")
			require.NoError(t, rig.erase(t.Context(), 1001, time.Now().Add(time.Hour)))
			require.Eventually(t, func() bool {
				name, err := rig.readers[0](t.Context(), 1001)
				return err == nil && name == ""
			}, time.Second, time.Millisecond)
			requireContentNames(t, rig, "")
		})
	}
}
