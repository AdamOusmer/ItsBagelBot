// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type apiReward struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Prompt    string `json:"prompt"`
	Cost      int    `json:"cost"`
	IsEnabled bool   `json:"is_enabled"`
}

type rewardsAPI struct {
	mu          sync.Mutex
	rewards     map[string]*apiReward
	unmanaged   map[string]bool
	patchStatus map[string]int
	gets        int
	patches     []string
	onPatch     func()
}

func newRewardsAPI(rewards ...apiReward) *rewardsAPI {
	api := &rewardsAPI{rewards: map[string]*apiReward{}, unmanaged: map[string]bool{}, patchStatus: map[string]int{}}
	for _, r := range rewards {
		api.rewards[r.ID] = &r
	}
	return api
}

func (a *rewardsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.get(w, r)
	case http.MethodPatch:
		a.patch(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *rewardsAPI) get(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gets++
	id := r.URL.Query().Get("id")
	reward, found := a.rewards[id]
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	data := []apiReward{}
	if !a.unmanaged[id] {
		data = append(data, *reward)
	}
	writeRewards(w, data)
}

func (a *rewardsAPI) patch(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	var body strings.Builder
	buf := make([]byte, 512)
	n, _ := r.Body.Read(buf)
	body.Write(buf[:n])
	a.mu.Lock()
	a.patches = append(a.patches, id+" "+body.String())
	hook := a.onPatch
	a.mu.Unlock()
	if hook != nil {
		hook()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if status := a.patchStatus[id]; status != 0 {
		w.WriteHeader(status)
		return
	}
	reward, found := a.rewards[id]
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = codec.Unmarshal([]byte(body.String()), reward)
	writeRewards(w, []apiReward{*reward})
}

func writeRewards(w http.ResponseWriter, data []apiReward) {
	body, _ := codec.Marshal(struct {
		Data []apiReward `json:"data"`
	}{data})
	_, _ = w.Write(body)
}

func (a *rewardsAPI) state(id string) apiReward {
	a.mu.Lock()
	defer a.mu.Unlock()
	return *a.rewards[id]
}

func (a *rewardsAPI) calls() ([]string, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.patches...), a.gets
}

func (a *rewardsAPI) resetCalls() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.patches, a.gets = nil, 0
}

func (a *rewardsAPI) setStatus(id string, status int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.patchStatus[id] = status
}

func withRewardSnapshots(vc valkey.Client) workerOption {
	return func(c *Config) { c.RewardSnapshots = NewRewardSnapshots(vc) }
}

func rewardsWorker(t *testing.T, api *rewardsAPI, vc valkey.Client, opts ...workerOption) *Worker {
	t.Helper()
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	opts = append([]workerOption{withRewardSnapshots(vc)}, opts...)
	return pipelineWorker(t, rehostTransport{base: server.Client().Transport, target: target}, opts...)
}

func sendRewardUpdate(w *Worker, rewardID, payload string) error {
	body := `{"type":"reward_update","broadcaster_id":"` + testBroadcaster + `","reward_id":"` + rewardID + `","payload":` + payload + `}`
	return w.Process(bus.NewMessage("reward-update", []byte(body)))
}

func sendRewardRestore(w *Worker) error {
	return testMessage{Type: outgress.TypeRewardRestore}.send(w)
}

func snapshotFields(t *testing.T, vc valkey.Client) map[string]string {
	t.Helper()
	fields, err := vc.Do(context.Background(), vc.B().Hgetall().Key(outgress.RewardSnapshotKey(testBroadcaster)).Build()).AsStrMap()
	require.NoError(t, err)
	return fields
}

func hydrate() apiReward {
	return apiReward{ID: "rw-1", Title: "Hydrate", Prompt: "Drink", Cost: 100, IsEnabled: true}
}

func TestRewardUpdateSnapshotsFirstChangeOnly(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)

	require.NoError(t, sendRewardUpdate(w, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))
	require.NoError(t, sendRewardUpdate(w, "rw-1", `{"enabled":false,"cost":25,"reset_on_stream_end":true}`))

	_, gets := api.calls()
	assert.Equal(t, 1, gets, "later changes must not read the already changed state")
	assert.Equal(t, 25, api.state("rw-1").Cost)
	assert.False(t, api.state("rw-1").IsEnabled)

	require.NoError(t, sendRewardRestore(w))

	assert.Equal(t, hydrate(), api.state("rw-1"))
	assert.Empty(t, snapshotFields(t, vc))
}

func TestRewardUpdateSnapshotExpiresAfterThirtyDays(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	require.NoError(t, sendRewardUpdate(rewardsWorker(t, api, vc), "rw-1", `{"cost":50,"reset_on_stream_end":true}`))

	ttl, err := vc.Do(context.Background(), vc.B().Ttl().Key(outgress.RewardSnapshotKey(testBroadcaster)).Build()).AsInt64()

	require.NoError(t, err)
	assert.InDelta(t, (30 * 24 * time.Hour).Seconds(), ttl, 60)
}

func TestRewardUpdateWithoutFlagDoesNotSnapshot(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)

	require.NoError(t, sendRewardUpdate(w, "rw-1", `{"cost":50}`))

	patches, gets := api.calls()
	assert.Zero(t, gets)
	assert.Equal(t, []string{`rw-1 {"cost":50}`}, patches)
	assert.Empty(t, snapshotFields(t, vc))

	api.resetCalls()
	require.NoError(t, sendRewardRestore(w))
	patches, _ = api.calls()
	assert.Empty(t, patches)
	assert.Equal(t, 50, api.state("rw-1").Cost)
}

func TestRewardUpdatePatchesOnlyTheChangedFields(t *testing.T) {
	cases := map[string]struct {
		payload string
		want    string
	}{
		"cost leaves enabled alone": {`{"cost":50,"reset_on_stream_end":true}`, `rw-1 {"cost":50}`},
		"enabled leaves cost alone": {`{"enabled":false}`, `rw-1 {"is_enabled":false}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := newRewardsAPI(hydrate())

			require.NoError(t, sendRewardUpdate(rewardsWorker(t, api, valkeytest.Spawn(t)), "rw-1", tc.payload))

			patches, _ := api.calls()
			assert.Equal(t, []string{tc.want}, patches)
			assert.Equal(t, "Hydrate", api.state("rw-1").Title)
			assert.Equal(t, "Drink", api.state("rw-1").Prompt)
		})
	}
}

func TestRewardRestoreAppliesSnapshotOnce(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)
	require.NoError(t, sendRewardUpdate(w, "rw-1", `{"enabled":false,"reset_on_stream_end":true}`))
	api.resetCalls()

	require.NoError(t, sendRewardRestore(w))
	require.NoError(t, sendRewardRestore(w))

	patches, _ := api.calls()
	assert.Equal(t, []string{`rw-1 {"is_enabled":true,"cost":100}`}, patches)
}

func TestRewardRestoreRunsOnceAcrossConcurrentReplicas(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	first, second := rewardsWorker(t, api, vc), rewardsWorker(t, api, vc)
	require.NoError(t, sendRewardUpdate(first, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))
	api.resetCalls()

	inPatch, release := make(chan struct{}), make(chan struct{})
	api.onPatch = func() {
		close(inPatch)
		<-release
	}
	done := make(chan error, 1)
	go func() { done <- sendRewardRestore(first) }()
	<-inPatch

	require.NoError(t, sendRewardRestore(second))
	close(release)
	require.NoError(t, <-done)

	patches, _ := api.calls()
	assert.Len(t, patches, 1)
	assert.Empty(t, snapshotFields(t, vc))
}

func TestFailedRewardRestoreKeepsSnapshotForNextOffline(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)
	require.NoError(t, sendRewardUpdate(w, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))

	api.setStatus("rw-1", http.StatusInternalServerError)
	require.NoError(t, sendRewardRestore(w))
	assert.Contains(t, snapshotFields(t, vc), "rw-1")
	assert.Equal(t, 50, api.state("rw-1").Cost)

	api.setStatus("rw-1", 0)
	require.NoError(t, sendRewardRestore(w))
	assert.Empty(t, snapshotFields(t, vc))
	assert.Equal(t, 100, api.state("rw-1").Cost)
}

func TestDeletedRewardDoesNotWedgeTheOtherRestores(t *testing.T) {
	second := apiReward{ID: "rw-2", Title: "Stretch", Cost: 200, IsEnabled: false}
	third := apiReward{ID: "rw-3", Title: "Dance", Cost: 300, IsEnabled: true}
	api, vc := newRewardsAPI(hydrate(), second, third), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)
	for _, id := range []string{"rw-1", "rw-2", "rw-3"} {
		require.NoError(t, sendRewardUpdate(w, id, `{"cost":5,"reset_on_stream_end":true}`))
	}
	api.setStatus("rw-2", http.StatusNotFound)
	api.setStatus("rw-3", http.StatusInternalServerError)

	require.NoError(t, sendRewardRestore(w))

	assert.Equal(t, hydrate(), api.state("rw-1"))
	remaining := snapshotFields(t, vc)
	assert.NotContains(t, remaining, "rw-2", "a deleted reward can never restore, so its snapshot goes")
	assert.Contains(t, remaining, "rw-3", "a failed restore stays for the next offline")
	assert.Len(t, remaining, 1)
}

func TestRewardUpdateSurvivesRedeliveryWithoutResnapshotting(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)
	api.setStatus("rw-1", http.StatusBadGateway)

	require.Error(t, sendRewardUpdate(w, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))

	api.setStatus("rw-1", 0)
	require.NoError(t, sendRewardUpdate(w, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))
	_, gets := api.calls()
	assert.Equal(t, 1, gets)
	require.NoError(t, sendRewardRestore(w))
	assert.Equal(t, 100, api.state("rw-1").Cost)
}

func TestRewardNotManageableOnlyReachesLogs(t *testing.T) {
	cases := map[string]struct {
		payload string
		arrange func(*rewardsAPI)
		patches int
	}{
		"read filtered by client id": {`{"cost":50,"reset_on_stream_end":true}`, func(a *rewardsAPI) { a.unmanaged["rw-1"] = true }, 0},
		"patch refused":              {`{"cost":50}`, func(a *rewardsAPI) { a.setStatus("rw-1", http.StatusForbidden) }, 1},
		"reward deleted before read": {`{"cost":50,"reset_on_stream_end":true}`, func(a *rewardsAPI) { delete(a.rewards, "rw-1") }, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			core, logs := observer.New(zap.WarnLevel)
			api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
			tc.arrange(api)
			w := rewardsWorker(t, api, vc, withLog(zap.New(core)))

			require.NoError(t, sendRewardUpdate(w, "rw-1", tc.payload), "a refusal must not redeliver")

			patches, _ := api.calls()
			assert.Len(t, patches, tc.patches)
			assert.Empty(t, snapshotFields(t, vc))
			assert.Equal(t, 1, logs.FilterMessage("dropping reward update: "+rewardNotManageable).Len())
		})
	}
}

func TestRewardUpdateWithBrokenSnapshotStoreDoesNotPatch(t *testing.T) {
	srv := valkeytest.New(t)
	srv.Fail(valkeytest.Failure{Cmd: "HEXISTS", Message: "ERR read refused"})
	api := newRewardsAPI(hydrate())
	w := rewardsWorker(t, api, srv.Client())

	require.Error(t, sendRewardUpdate(w, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))

	patches, _ := api.calls()
	assert.Empty(t, patches, "a change that cannot be put back must wait")
}
