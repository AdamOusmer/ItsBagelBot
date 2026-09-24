// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

func TestAsyncPublishTimeoutExpiresOnlyTheLostFuture(t *testing.T) {
	t.Setenv("NATS_PUBLISH_ACK_WAIT", "1s")
	t.Setenv("NATS_HUB_URL", "")
	t.Setenv("NATS_HUB_PUBLISH_URL", "")
	peer := newPublishTimeoutPeer(t)
	defer peer.close()
	var releaseSecond sync.Once
	defer releaseSecond.Do(func() { close(peer.releaseSecond) })

	publisher := newTimeoutPublisher(t, peer)
	defer publisher.nc.Close()

	worker := &publishBatchWorker{js: publisher.js}
	first := startTimeoutPublish(t, worker, "review.timeout.first")
	secondResult := startSecondTimeoutPublish(worker)
	awaitLostTimeout(t, worker, first)
	second := receiveAsyncResult(t, secondResult)
	peer.waitForSecondPublish(t)

	waitForPendingFuture(t, publisher.js, 1)
	releaseSecond.Do(func() { close(peer.releaseSecond) })
	awaitSecondAck(t, second.futures[0])

	third := startTimeoutPublish(t, worker, "review.timeout.third")
	if err := worker.awaitAsync(third); err != nil {
		t.Fatalf("publish after lost PubAck await error = %v", err)
	}
}

type asyncResult struct {
	futures []nats.PubAckFuture
	err     error
}

func newTimeoutPublisher(t *testing.T, peer *publishTimeoutPeer) *batchPublisher {
	t.Helper()
	publisher, err := newBatchPublisherConnection("nats://"+peer.listener.Addr().String(), 0, wireSingle, zap.NewNop())
	if err != nil {
		t.Fatalf("newBatchPublisherConnection() error = %v", err)
	}
	return publisher
}

func startTimeoutPublish(t *testing.T, worker *publishBatchWorker, subject string) []nats.PubAckFuture {
	t.Helper()
	futures, err := worker.startAsync([]publishRequest{{msg: nats.NewMsg(subject)}})
	if err != nil {
		t.Fatalf("PublishMsgAsync(%q) error = %v", subject, err)
	}
	return futures
}

func startSecondTimeoutPublish(worker *publishBatchWorker) <-chan asyncResult {
	result := make(chan asyncResult, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		futures, err := worker.startAsync([]publishRequest{{msg: nats.NewMsg("review.timeout.second")}})
		result <- asyncResult{futures: futures, err: err}
	}()
	return result
}

func awaitLostTimeout(t *testing.T, worker *publishBatchWorker, futures []nats.PubAckFuture) {
	t.Helper()
	started := time.Now()
	err := worker.awaitAsync(futures)
	elapsed := time.Since(started)
	if !isPublishTimeout(err) {
		t.Fatalf("lost PubAck await error = %v, want timeout", err)
	}
	if elapsed < 800*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("NATS_PUBLISH_ACK_WAIT=1s waited %v, want about one second", elapsed)
	}
}

func isPublishTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, nats.ErrAsyncPublishTimeout) {
		return true
	}
	return strings.Contains(err.Error(), "PubAck timeout")
}

func receiveAsyncResult(t *testing.T, result <-chan asyncResult) asyncResult {
	t.Helper()
	select {
	case second := <-result:
		if second.err != nil {
			t.Fatalf("second PublishMsgAsync() error = %v", second.err)
		}
		return second
	case <-time.After(time.Second):
		t.Fatal("second PublishMsgAsync() did not start")
		return asyncResult{}
	}
}

func waitForPendingFuture(t *testing.T, js nats.JetStreamContext, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for js.PublishAsyncPending() != want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := js.PublishAsyncPending(); got != want {
		t.Fatalf("pending futures after first expiry = %d, want only the second future", got)
	}
}

func awaitSecondAck(t *testing.T, future nats.PubAckFuture) {
	t.Helper()
	select {
	case <-future.Ok():
	case err := <-future.Err():
		t.Fatalf("unrelated in-flight PubAck failed after first expired: %v", err)
	case <-time.After(time.Second):
		t.Fatal("unrelated in-flight PubAck did not arrive")
	}
}

type publishTimeoutPeer struct {
	listener      net.Listener
	secondSeen    chan struct{}
	releaseSecond chan struct{}
	done          chan struct{}
	conn          net.Conn
	reader        *bufio.Reader
	publishes     int
}

func newPublishTimeoutPeer(t *testing.T) *publishTimeoutPeer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	peer := &publishTimeoutPeer{
		listener: listener, secondSeen: make(chan struct{}), releaseSecond: make(chan struct{}), done: make(chan struct{}),
	}
	go peer.serve(t)
	return peer
}

func (p *publishTimeoutPeer) close() {
	p.listener.Close()
	select {
	case <-p.done:
	case <-time.After(time.Second):
	}
}

func (p *publishTimeoutPeer) waitForSecondPublish(t *testing.T) {
	t.Helper()
	select {
	case <-p.secondSeen:
	case <-time.After(time.Second):
		t.Fatal("second publish did not reach protocol peer")
	}
}

func (p *publishTimeoutPeer) serve(t *testing.T) {
	defer close(p.done)
	conn, err := p.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	fmt.Fprint(conn, "INFO {\"server_id\":\"publish-timeout\",\"version\":\"2.14.4\",\"proto\":1,\"headers\":true,\"max_payload\":1048576}\r\n")

	p.reader = bufio.NewReader(conn)
	p.conn = conn
	for {
		line, err := p.reader.ReadString('\n')
		if err != nil {
			return
		}
		if !p.handleProtocolLine(t, line) {
			return
		}
	}
}

func (p *publishTimeoutPeer) handleProtocolLine(t *testing.T, line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return true
	}
	if fields[0] == "PING" {
		fmt.Fprint(p.conn, "PONG\r\n")
		return true
	}
	if fields[0] != "PUB" && fields[0] != "HPUB" {
		return true
	}
	return p.handlePublish(t, line, fields)
}

func (p *publishTimeoutPeer) handlePublish(t *testing.T, line string, fields []string) bool {
	reply, sizeField, ok := publishTimeoutEnvelope(fields)
	if !ok {
		t.Errorf("publish fields = %q", line)
		return false
	}
	size, err := strconv.Atoi(sizeField)
	if err != nil {
		t.Errorf("publish size %q: %v", sizeField, err)
		return false
	}
	if _, err := p.reader.Discard(size + 2); err != nil {
		return false
	}
	p.publishes++
	return p.respondToPublish(reply)
}

func (p *publishTimeoutPeer) respondToPublish(reply string) bool {
	switch p.publishes {
	case 1:
	case 2:
		close(p.secondSeen)
		<-p.releaseSecond
		publishTimeoutAck(p.conn, reply, 2)
	default:
		publishTimeoutAck(p.conn, reply, uint64(p.publishes))
	}
	return true
}

func publishTimeoutEnvelope(fields []string) (reply, size string, ok bool) {
	switch fields[0] {
	case "PUB":
		if len(fields) == 4 {
			return fields[2], fields[3], true
		}
	case "HPUB":
		if len(fields) == 5 {
			return fields[2], fields[4], true
		}
	}
	return "", "", false
}

func publishTimeoutAck(conn net.Conn, reply string, sequence uint64) {
	body := fmt.Sprintf("{\"stream\":\"REVIEW\",\"seq\":%d}", sequence)
	fmt.Fprintf(conn, "MSG %s 1 %d\r\n%s\r\n", reply, len(body), body)
}
