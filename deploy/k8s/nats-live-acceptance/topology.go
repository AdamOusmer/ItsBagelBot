package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

type topologyPeer struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Current bool   `json:"current"`
	Offline bool   `json:"offline"`
	Lag     uint64 `json:"lag"`
}

type topologyReport struct {
	InitialLeader            string         `json:"initial_leader"`
	Leader                   string         `json:"leader"`
	Peers                    []topologyPeer `json:"peers"`
	Samples                  int            `json:"samples"`
	LeaderChanges            int            `json:"leader_changes"`
	LeaderElectionAdvisories int            `json:"leader_election_advisories"`
	ElectedLeaders           []string       `json:"elected_leaders,omitempty"`
	PeakFollowerLag          uint64         `json:"peak_follower_lag"`
	ForbiddenLeader          string         `json:"forbidden_leader,omitempty"`
	ForbiddenLeaderSeen      bool           `json:"forbidden_leader_seen"`
	ForbiddenFollowerCurrent bool           `json:"forbidden_follower_current"`
	AllCurrent               bool           `json:"all_current"`
	FollowerLagZero          bool           `json:"follower_lag_zero"`
	Reconnects               int64          `json:"reconnects"`
	Disconnects              int64          `json:"disconnects"`
	AsyncErrors              int64          `json:"async_errors"`
	Passed                   bool           `json:"passed"`
	Failure                  string         `json:"failure,omitempty"`
}

type topologyObserver struct {
	report topologyReport
}

type topologyMonitor struct {
	cfg        config
	setup      client
	stream     jsapi.Stream
	advisories *leaderAdvisoryWatch
	observer   topologyObserver
}

type leaderAdvisoryWatch struct {
	stream string
	sub    *nats.Subscription

	mu       sync.Mutex
	leaders  []string
	firstErr error
}

type leaderAdvisoryResult struct {
	leaders []string
	err     error
}

const streamLeaderAdvisoryPrefix = "$JS.EVENT.ADVISORY.STREAM.LEADER_ELECTED."

func monitorStreamTopology(cfg config, setup client) (topologyReport, error) {
	report := topologyReport{ForbiddenLeader: cfg.forbiddenLeader}
	monitor, err := newTopologyMonitor(cfg, setup, report)
	if err != nil {
		return failedTopology(report, err)
	}
	return monitor.run()
}

func newTopologyMonitor(cfg config, setup client, report topologyReport) (*topologyMonitor, error) {
	if err := validateTopologyTarget(cfg); err != nil {
		return nil, err
	}
	stream, err := preparedTopologyStream(cfg, setup)
	if err != nil {
		return nil, err
	}
	advisories, err := startLeaderAdvisoryWatch(cfg, setup.nc)
	if err != nil {
		return nil, err
	}
	return &topologyMonitor{
		cfg: cfg, setup: setup, stream: stream, advisories: advisories,
		observer: topologyObserver{report: report},
	}, nil
}

func validateTopologyTarget(cfg config) error {
	if err := validateTemporaryTarget(cfg.stream, cfg.subject); err != nil {
		return err
	}
	return validateR3ShadowConfig(cfg)
}

func preparedTopologyStream(cfg config, setup client) (jsapi.Stream, error) {
	stream, err := lookupStream(setup.modern, cfg)
	if err != nil {
		return nil, err
	}
	if err := prepareTopology(cfg, setup.nc, stream); err != nil {
		return nil, err
	}
	return stream, nil
}

func (m *topologyMonitor) run() (topologyReport, error) {
	if !waitForTopologyStart(m.cfg.startAtUnixMS) {
		return m.failWithAdvisories(errors.New("topology start barrier is invalid"))
	}
	if err := m.observeLoad(); err != nil {
		return m.failWithAdvisories(err)
	}
	if err := m.finish(); err != nil {
		return failedTopology(m.observer.report, err)
	}
	m.observer.report.Passed = true
	return m.observer.report, nil
}

func (m *topologyMonitor) observeLoad() error {
	if err := observeForDuration(m.cfg, m.stream, &m.observer); err != nil {
		return err
	}
	finalInfo, err := waitForTopologyReady(m.cfg, m.stream)
	if err != nil {
		return err
	}
	return m.observer.observe(finalInfo)
}

func (m *topologyMonitor) finish() error {
	if err := finishLeaderAdvisories(m.cfg, m.advisories, &m.observer.report); err != nil {
		return err
	}
	m.observer.finish(m.cfg, m.setup.stats)
	return validateTopologyReport(m.observer.report)
}

func (m *topologyMonitor) failWithAdvisories(runErr error) (topologyReport, error) {
	advisoryErr := finishLeaderAdvisories(m.cfg, m.advisories, &m.observer.report)
	return failedTopology(m.observer.report, errors.Join(runErr, advisoryErr))
}

func startLeaderAdvisoryWatch(cfg config, nc *nats.Conn) (*leaderAdvisoryWatch, error) {
	watch := &leaderAdvisoryWatch{stream: cfg.stream}
	sub, err := nc.Subscribe(streamLeaderAdvisoryPrefix+cfg.stream, watch.observe)
	if err != nil {
		return nil, fmt.Errorf("subscribe to stream leader advisories: %w", err)
	}
	watch.sub = sub
	if err := nc.FlushTimeout(cfg.ackTimeout); err != nil {
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("activate stream leader advisory subscription: %w", err)
	}
	return watch, nil
}

func (w *leaderAdvisoryWatch) observe(msg *nats.Msg) {
	var advisory struct {
		Stream string `json:"stream"`
		Leader string `json:"leader"`
	}
	err := json.Unmarshal(msg.Data, &advisory)
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		if w.firstErr == nil {
			w.firstErr = fmt.Errorf("decode stream leader advisory: %w", err)
		}
		return
	}
	if advisory.Stream != w.stream || advisory.Leader == "" {
		if w.firstErr == nil {
			w.firstErr = fmt.Errorf(
				"invalid stream leader advisory: stream=%q leader=%q",
				advisory.Stream, advisory.Leader,
			)
		}
		return
	}
	w.leaders = append(w.leaders, advisory.Leader)
}

func finishLeaderAdvisories(cfg config, watch *leaderAdvisoryWatch, report *topologyReport) error {
	result := watch.stop(cfg.ackTimeout)
	applyLeaderAdvisoryResult(report, result)
	return result.err
}

func applyLeaderAdvisoryResult(report *topologyReport, result leaderAdvisoryResult) {
	report.LeaderElectionAdvisories = len(result.leaders)
	report.ElectedLeaders = result.leaders
	for _, leader := range result.leaders {
		if leader == report.ForbiddenLeader {
			report.ForbiddenLeaderSeen = true
		}
	}
}

func (w *leaderAdvisoryWatch) stop(timeout time.Duration) leaderAdvisoryResult {
	closed := w.sub.StatusChanged(nats.SubscriptionClosed)
	if err := w.sub.Drain(); err != nil {
		return w.snapshot(fmt.Errorf("drain stream leader advisories: %w", err))
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-closed:
		return w.snapshot(nil)
	case <-timer.C:
		_ = w.sub.Unsubscribe()
		return w.snapshot(errors.New("timed out draining stream leader advisories"))
	}
}

func (w *leaderAdvisoryWatch) snapshot(stopErr error) leaderAdvisoryResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	return leaderAdvisoryResult{
		leaders: append([]string(nil), w.leaders...),
		err:     errors.Join(w.firstErr, stopErr),
	}
}

func lookupStream(js jsapi.JetStream, cfg config) (jsapi.Stream, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ackTimeout)
	defer cancel()
	stream, err := js.Stream(ctx, cfg.stream)
	if err != nil {
		return nil, fmt.Errorf("lookup shadow stream %s: %w", cfg.stream, err)
	}
	return stream, nil
}

func prepareTopology(cfg config, nc *nats.Conn, stream jsapi.Stream) error {
	info, err := streamInfo(stream, cfg.ackTimeout)
	if err != nil {
		return err
	}
	if cfg.forbiddenLeader != "" && info.Cluster != nil && info.Cluster.Leader == cfg.forbiddenLeader {
		if cfg.preferredLeader == "" {
			return fmt.Errorf("forbidden server %s leads and no preferred leader was provided", cfg.forbiddenLeader)
		}
		if err := requestLeaderStepdown(cfg, nc); err != nil {
			return err
		}
	}
	_, err = waitForTopologyReady(cfg, stream)
	return err
}

func requestLeaderStepdown(cfg config, nc *nats.Conn) error {
	if !safeServerName(cfg.preferredLeader) {
		return fmt.Errorf("unsafe preferred leader %q", cfg.preferredLeader)
	}
	payload, err := json.Marshal(map[string]any{
		"placement": map[string]string{"preferred": cfg.preferredLeader},
	})
	if err != nil {
		return err
	}
	subject := jetStreamAPIPrefix(cfg.domain) + ".STREAM.LEADER.STEPDOWN." + cfg.stream
	reply, err := nc.Request(subject, payload, cfg.ackTimeout)
	if err != nil {
		return fmt.Errorf("request leader stepdown to %s: %w", cfg.preferredLeader, err)
	}
	var response struct {
		Error *jsapi.APIError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(reply.Data, &response); err != nil {
		return fmt.Errorf("decode leader stepdown response: %w", err)
	}
	if response.Error != nil {
		return fmt.Errorf("leader stepdown rejected: %s", response.Error.Description)
	}
	return nil
}

func jetStreamAPIPrefix(domain string) string {
	if domain == "" {
		return "$JS.API"
	}
	return "$JS." + domain + ".API"
}

func safeServerName(name string) bool {
	return name != "" && !strings.ContainsAny(name, ".*> \t\r\n")
}

func waitForTopologyReady(cfg config, stream jsapi.Stream) (*jsapi.StreamInfo, error) {
	deadline := time.Now().Add(cfg.settleTimeout)
	var last *jsapi.StreamInfo
	for time.Now().Before(deadline) {
		info, err := streamInfo(stream, cfg.ackTimeout)
		if err == nil {
			last = info
			if topologyReady(info, cfg.requiredPeers, cfg.forbiddenLeader) {
				return info, nil
			}
		}
		time.Sleep(min(cfg.topologyInterval, 250*time.Millisecond))
	}
	if last == nil {
		return nil, errors.New("stream topology never became readable")
	}
	return nil, fmt.Errorf("stream topology did not settle: %s", topologyState(last))
}

func streamInfo(stream jsapi.Stream, timeout time.Duration) (*jsapi.StreamInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return stream.Info(ctx)
}

func topologyReady(info *jsapi.StreamInfo, requiredPeers int, forbiddenLeader string) bool {
	if info == nil || info.Cluster == nil || info.Cluster.Leader == "" {
		return false
	}
	cluster := info.Cluster
	if 1+len(cluster.Replicas) != requiredPeers || cluster.Leader == forbiddenLeader {
		return false
	}
	forbiddenFollowerCurrent := forbiddenLeader == ""
	for _, peer := range cluster.Replicas {
		if peer.Offline || !peer.Current || peer.Lag != 0 {
			return false
		}
		if peer.Name == forbiddenLeader {
			forbiddenFollowerCurrent = true
		}
	}
	return forbiddenFollowerCurrent
}

func topologyState(info *jsapi.StreamInfo) string {
	if info == nil || info.Cluster == nil {
		return "cluster metadata unavailable"
	}
	parts := []string{"leader=" + info.Cluster.Leader}
	for _, peer := range info.Cluster.Replicas {
		parts = append(parts, fmt.Sprintf("%s(current=%t offline=%t lag=%d)", peer.Name, peer.Current, peer.Offline, peer.Lag))
	}
	return strings.Join(parts, " ")
}

func waitForTopologyStart(unixMS int64) bool {
	if unixMS <= 0 {
		return true
	}
	wait := time.Until(time.UnixMilli(unixMS))
	if wait < -time.Second {
		return false
	}
	if wait > 0 {
		time.Sleep(wait)
	}
	return true
}

func observeForDuration(cfg config, stream jsapi.Stream, observer *topologyObserver) error {
	deadline := time.Now().Add(cfg.topologyDuration)
	for {
		info, err := streamInfo(stream, cfg.ackTimeout)
		if err != nil {
			return fmt.Errorf("sample stream topology: %w", err)
		}
		if err := observer.observe(info); err != nil {
			return err
		}
		if cfg.topologyDuration == 0 || !time.Now().Before(deadline) {
			return nil
		}
		time.Sleep(min(cfg.topologyInterval, time.Until(deadline)))
	}
}

func (o *topologyObserver) observe(info *jsapi.StreamInfo) error {
	cluster, err := observedCluster(info)
	if err != nil {
		return err
	}
	leader := cluster.Leader
	o.recordLeader(leader)
	o.report.Samples++
	o.report.Peers = topologyPeers(cluster)
	if err := o.rejectForbiddenLeader(leader); err != nil {
		return err
	}
	o.recordPeakFollowerLag(cluster.Replicas)
	return nil
}

func observedCluster(info *jsapi.StreamInfo) (*jsapi.ClusterInfo, error) {
	if info == nil {
		return nil, errors.New("stream has no cluster metadata")
	}
	if info.Cluster == nil {
		return nil, errors.New("stream has no cluster metadata")
	}
	if info.Cluster.Leader == "" {
		return nil, errors.New("stream has no cluster leader")
	}
	return info.Cluster, nil
}

func (o *topologyObserver) recordLeader(leader string) {
	if o.report.InitialLeader == "" {
		o.report.InitialLeader = leader
	}
	if o.report.Leader != "" && o.report.Leader != leader {
		o.report.LeaderChanges++
	}
	o.report.Leader = leader
}

func (o *topologyObserver) rejectForbiddenLeader(leader string) error {
	if leader == o.report.ForbiddenLeader {
		o.report.ForbiddenLeaderSeen = true
		return fmt.Errorf("forbidden server %s became stream leader", leader)
	}
	return nil
}

func (o *topologyObserver) recordPeakFollowerLag(peers []*jsapi.PeerInfo) {
	for _, peer := range peers {
		o.report.PeakFollowerLag = max(o.report.PeakFollowerLag, peer.Lag)
	}
}

func topologyPeers(cluster *jsapi.ClusterInfo) []topologyPeer {
	peers := []topologyPeer{{Name: cluster.Leader, Role: "leader", Current: true}}
	for _, peer := range cluster.Replicas {
		peers = append(peers, topologyPeer{
			Name: peer.Name, Role: "follower", Current: peer.Current,
			Offline: peer.Offline, Lag: peer.Lag,
		})
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	return peers
}

func (o *topologyObserver) finish(cfg config, stats *connectionStats) {
	o.report.AllCurrent = allPeersCurrent(o.report.Peers, cfg.requiredPeers)
	o.report.FollowerLagZero = followerLagZero(o.report.Peers)
	o.report.ForbiddenFollowerCurrent = forbiddenFollowerCurrent(o.report.Peers, cfg.forbiddenLeader)
	o.report.Reconnects = stats.reconnects.Load()
	o.report.Disconnects = stats.disconnects.Load()
	o.report.AsyncErrors = stats.asyncErrors.Load()
}

func allPeersCurrent(peers []topologyPeer, required int) bool {
	if len(peers) != required {
		return false
	}
	for _, peer := range peers {
		if !peer.Current || peer.Offline {
			return false
		}
	}
	return true
}

func followerLagZero(peers []topologyPeer) bool {
	for _, peer := range peers {
		if peer.Role == "follower" && peer.Lag != 0 {
			return false
		}
	}
	return true
}

func forbiddenFollowerCurrent(peers []topologyPeer, forbidden string) bool {
	if forbidden == "" {
		return true
	}
	for _, peer := range peers {
		if currentFollower(peer, forbidden) {
			return true
		}
	}
	return false
}

func currentFollower(peer topologyPeer, name string) bool {
	if peer.Name != name || peer.Role != "follower" {
		return false
	}
	return peer.Current && !peer.Offline
}

func validateTopologyReport(report topologyReport) error {
	switch {
	case report.ForbiddenLeaderSeen:
		return errors.New("forbidden leader was observed")
	case report.LeaderElectionAdvisories > 0:
		return fmt.Errorf("observed %d stream leader election advisories under load", report.LeaderElectionAdvisories)
	case report.LeaderChanges > 0:
		return fmt.Errorf("stream leader changed %d times under load", report.LeaderChanges)
	case !report.AllCurrent:
		return errors.New("not all stream members are current")
	case !report.FollowerLagZero:
		return errors.New("follower lag did not return to zero")
	case !report.ForbiddenFollowerCurrent:
		return errors.New("forbidden leader server is not a current follower")
	case report.Reconnects+report.Disconnects+report.AsyncErrors > 0:
		return fmt.Errorf(
			"topology monitor connection instability: reconnects=%d disconnects=%d async_errors=%d",
			report.Reconnects, report.Disconnects, report.AsyncErrors,
		)
	default:
		return nil
	}
}

func failedTopology(report topologyReport, err error) (topologyReport, error) {
	report.Failure = err.Error()
	return report, err
}

func printTopologyReport(cfg config, report topologyReport) {
	out, err := json.MarshalIndent(map[string]any{
		"stream": cfg.stream, "subject": cfg.subject, "topology": report,
	}, "", "  ")
	if err != nil {
		fmt.Printf("{\"stream\":%q,\"topology_error\":%q}\n", cfg.stream, err.Error())
		return
	}
	fmt.Println(string(out))
}
