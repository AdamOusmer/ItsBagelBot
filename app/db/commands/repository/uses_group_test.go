// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/commands"
	"ItsBagelBot/app/db/commands/ent/enttest"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type useSQLTrace struct {
	mu              sync.Mutex
	mergedDuplicate bool
	queries         int
	inserts         int
	updates         int
}

func (s *useSQLTrace) writes() [2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return [2]int{s.inserts, s.updates}
}

func (s *useSQLTrace) log(args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line := fmt.Sprint(args...)
	if strings.Contains(line, "SELECT") {
		s.queries++
	}
	if strings.Contains(line, "INSERT INTO") {
		s.inserts++
	}
	if strings.Contains(line, "UPDATE") {
		s.updates++
	}
	if strings.Contains(line, "command_use_batches") && strings.Contains(line, "SELECT") {
		s.mergedDuplicate = s.mergedDuplicate || strings.Count(line, "shared-id") > 1
	}
}

func TestCommandUseGroupsSQLite(t *testing.T) {
	trace := &useSQLTrace{}
	client := testdb.Open(t, "command_use_groups", func(d, dsn string) *ent.Client {
		driver, err := entsql.Open(d, dsn)
		require.NoError(t, err)
		driver.DB().SetMaxOpenConns(1)
		return enttest.NewClient(t, enttest.WithOptions(ent.Driver(driver), ent.Debug(), ent.Log(trace.log)))
	})
	testCommandUseGroups(t, client, trace, 1)
}

type useRequest struct {
	id    string
	name  string
	count int64
	fail  bool
}

type useGroupFixture struct {
	client *ent.Client
	repos  []*repository.Commands
	pub    *bustest.Publisher
}

func newUseGroupFixture(t *testing.T, client *ent.Client, replicas int) useGroupFixture {
	t.Helper()
	pub := bustest.NewPublisher()
	f := useGroupFixture{client: client, pub: pub}
	for range replicas {
		r := repository.NewCommands(client, pub, nil, zap.NewNop())
		t.Cleanup(func() { r.Close(context.Background()) })
		f.repos = append(f.repos, r)
	}
	return f
}

func (f useGroupFixture) submit(t *testing.T, requests []useRequest) {
	t.Helper()
	start := make(chan struct{})
	errs := make([]error, len(requests))
	var wg sync.WaitGroup
	for i, request := range requests {
		wg.Go(func() {
			<-start
			errs[i] = f.repos[i%len(f.repos)].RecordUse(t.Context(), request.id, data.CommandUsedDTO{UserID: 1001, Name: request.name, Count: request.count})
		})
	}
	close(start)
	wg.Wait()
	for i, request := range requests {
		assert.Equal(t, request.fail, errs[i] != nil, "%s: %v", request.id, errs[i])
	}
}

func testCommandUseGroups(t *testing.T, client *ent.Client, trace *useSQLTrace, replicas int) {
	t.Helper()
	f := newUseGroupFixture(t, client, replicas)
	ctx := t.Context()
	client.Commands.Create().SetUserID(1001).SetName("hello").SetResponse("hi").SaveX(ctx)
	client.Commands.Create().SetUserID(1001).SetName("full").SetResponse("hi").SetUses(math.MaxInt64 - 1).SaveX(ctx)
	client.Commands.Create().SetUserID(1001).SetName("almost").SetResponse("hi").SetUses(math.MaxInt64 - 1).SaveX(ctx)
	requests := make([]useRequest, 256)
	for i := range requests {
		requests[i] = useRequest{id: fmt.Sprint("use-", i), name: "hello", count: 3}
	}
	f.submit(t, requests)
	require.Eventually(t, func() bool { return len(f.pub.On(data.SubjectCommandChanged)) > 0 }, time.Second, time.Millisecond)
	published := len(f.pub.On(data.SubjectCommandChanged))
	f.submit(t, requests)
	require.Eventually(t, func() bool { return len(f.pub.On(data.SubjectCommandChanged)) > published }, time.Second, time.Millisecond)
	assert.Equal(t, int64(768), f.total(t, "hello"))

	duplicates := make([]useRequest, 32)
	for i := range duplicates {
		duplicates[i] = useRequest{id: "shared-id", name: "hello", count: 7}
	}
	f.submit(t, duplicates)
	assert.Equal(t, int64(775), f.total(t, "hello"))
	assert.True(t, trace.mergedDuplicate, "same receipt ID must appear twice in a merged lookup")
	f.checkPoison(t)
	f.checkDistinctKeys(t, trace)
}

func (f useGroupFixture) checkDistinctKeys(t *testing.T, trace *useSQLTrace) {
	t.Helper()
	builders := make([]*ent.CommandsCreate, 128)
	requests := make([]useRequest, len(builders))
	want := make(map[string]int64, len(builders))
	for i := range builders {
		name := fmt.Sprint("key", i)
		builders[i] = f.client.Commands.Create().SetUserID(1001).SetName(name).SetResponse("hi")
		requests[i] = useRequest{id: "distinct-" + name, name: name, count: int64(i + 1)}
		want[name] = int64(i + 1)
	}
	require.NoError(t, f.client.Commands.CreateBulk(builders...).Exec(t.Context()))
	before := trace.writes()
	f.submit(t, requests)
	after := trace.writes()
	assert.Equal(t, after[0]-before[0], after[1]-before[1], "one counter UPDATE per receipt INSERT group")
	got := make(map[string]int64, len(builders))
	for _, row := range f.client.Commands.Query().Where(commands.NameHasPrefix("key")).AllX(t.Context()) {
		got[row.Name] = row.Uses
	}
	assert.Equal(t, want, got)
}

func (f useGroupFixture) total(t *testing.T, name string) int64 {
	t.Helper()
	return f.client.Commands.Query().Where(commands.UserIDEQ(1001), commands.NameEQ(name)).OnlyX(t.Context()).Uses
}

func (f useGroupFixture) checkPoison(t *testing.T) {
	t.Helper()
	requests := []useRequest{
		{id: "overflow", name: "full", count: 2, fail: true},
		{id: "shared-id", name: "hello", count: 9, fail: true},
		{id: "missing", name: "deleted", count: 4},
		{id: "boundary", name: "almost", count: 1},
	}
	for i := range 32 {
		requests = append(requests, useRequest{id: fmt.Sprint("healthy-", i), name: "hello", count: 1})
	}
	f.submit(t, requests)
	for _, tc := range []struct {
		name string
		want int64
	}{{"hello", 807}, {"full", math.MaxInt64 - 1}, {"almost", math.MaxInt64}} {
		assert.Equal(t, tc.want, f.total(t, tc.name))
	}
	_, err := f.client.CommandUseBatch.Get(t.Context(), "overflow")
	assert.True(t, ent.IsNotFound(err))
	assert.Equal(t, 291, f.client.CommandUseBatch.Query().CountX(t.Context()))
	f.client.Commands.Create().SetUserID(1001).SetName("deleted").SetResponse("new").SaveX(t.Context())
	f.submit(t, []useRequest{{id: "missing", name: "deleted", count: 4}})
	assert.Zero(t, f.total(t, "deleted"))
	require.NoError(t, f.repos[0].RecordUse(t.Context(), "last-unit", data.CommandUsedDTO{UserID: 1001, Name: "full", Count: 1}))
}
