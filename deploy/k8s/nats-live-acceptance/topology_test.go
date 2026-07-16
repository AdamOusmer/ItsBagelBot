package main

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestTopologyRequiresWorkerFollowerAndZeroLag(t *testing.T) {
	info := healthyTopology()
	if !topologyReady(info, 3, "nats-2") {
		t.Fatal("healthy R3 topology did not pass")
	}
	info.Cluster.Leader = "nats-2"
	if topologyReady(info, 3, "nats-2") {
		t.Fatal("worker1 server passed as leader")
	}
	info = healthyTopology()
	info.Cluster.Replicas[1].Lag = 1
	if topologyReady(info, 3, "nats-2") {
		t.Fatal("lagging worker1 follower passed")
	}
}

func TestPreferredLeaderMustMatchExactly(t *testing.T) {
	info := healthyTopology()
	require.True(t, preferredLeaderActive(info, "nats-0"))
	require.False(t, preferredLeaderActive(info, "nats-1"))
	require.True(t, preferredLeaderActive(info, ""))
	require.False(t, preferredLeaderActive(nil, "nats-0"))
}

func TestTopologyObserverDetectsLeaderChange(t *testing.T) {
	observer := topologyObserver{report: topologyReport{ForbiddenLeader: "nats-2"}}
	if err := observer.observe(healthyTopology()); err != nil {
		t.Fatal(err)
	}
	changed := healthyTopology()
	changed.Cluster.Leader = "nats-1"
	changed.Cluster.Replicas[0].Name = "nats-0"
	if err := observer.observe(changed); err != nil {
		t.Fatal(err)
	}
	if observer.report.LeaderChanges != 1 {
		t.Fatalf("leader changes = %d", observer.report.LeaderChanges)
	}
}

func TestTopologyObserverRejectsTransientUnhealthyFollower(t *testing.T) {
	for _, mutate := range []func(*jsapi.PeerInfo){
		func(peer *jsapi.PeerInfo) { peer.Current = false },
		func(peer *jsapi.PeerInfo) { peer.Offline = true },
	} {
		info := healthyTopology()
		mutate(info.Cluster.Replicas[0])
		observer := topologyObserver{report: topologyReport{ForbiddenLeader: "nats-2"}}
		require.ErrorContains(t, observer.observe(info), "became unhealthy")
	}
}

func TestTopologyObserverAllowsBoundedFollowerCatchup(t *testing.T) {
	started := time.Now()
	observer := topologyObserver{
		report:         topologyReport{ForbiddenLeader: "nats-2"},
		unhealthyGrace: 5 * time.Second,
		unhealthySince: make(map[string]time.Time),
	}
	info := healthyTopology()
	info.Cluster.Replicas[0].Current = false
	require.NoError(t, observer.rejectUnhealthyFollowers(info.Cluster.Replicas, started))
	require.NoError(t, observer.rejectUnhealthyFollowers(info.Cluster.Replicas, started.Add(4*time.Second)))
	require.ErrorContains(
		t,
		observer.rejectUnhealthyFollowers(info.Cluster.Replicas, started.Add(5*time.Second)),
		"became unhealthy",
	)

	info.Cluster.Replicas[0].Current = true
	require.NoError(t, observer.rejectUnhealthyFollowers(info.Cluster.Replicas, started.Add(6*time.Second)))
	require.Empty(t, observer.unhealthySince)
}

func TestLeaderAdvisoryDetectsTransientForbiddenLeader(t *testing.T) {
	watch := &leaderAdvisoryWatch{stream: "R3_SHADOW_TEST"}
	watch.observe(&nats.Msg{Data: []byte(`{"stream":"R3_SHADOW_TEST","leader":"nats-2"}`)})
	result := watch.snapshot(nil)
	report := topologyReport{ForbiddenLeader: "nats-2"}
	applyLeaderAdvisoryResult(&report, result)
	if !report.ForbiddenLeaderSeen || report.LeaderElectionAdvisories != 1 {
		t.Fatalf("transient leader advisory was not captured: %+v", report)
	}
	if err := validateTopologyReport(report); err == nil {
		t.Fatal("forbidden transient leader advisory passed topology validation")
	}
}
func healthyTopology() *jsapi.StreamInfo {
	return &jsapi.StreamInfo{Cluster: &jsapi.ClusterInfo{
		Leader: "nats-0",
		Replicas: []*jsapi.PeerInfo{
			{Name: "nats-1", Current: true},
			{Name: "nats-2", Current: true},
		},
	}}
}
