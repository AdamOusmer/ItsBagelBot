// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// --- test double: a real client against a scripted TCP server ---

// recentScriptedServer answers the client handshake with the one error the
// RESP2 fallback tolerates, replies to ZRANGEBYSCORE with the currently
// scripted member array, and acks everything else. It exists because sweep
// results must come off the WIRE (ValkeyMessage arrays cannot be synthesized
// from outside the library).
type recentScriptedServer struct {
	ln net.Listener

	mu        sync.Mutex
	members   []string
	lastRange []string
}

func newRecentScriptedServer(tb testing.TB) *recentScriptedServer {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(tb, err)
	tb.Cleanup(func() { ln.Close() })
	s := &recentScriptedServer{ln: ln}
	go s.serve()
	return s
}

func (s *recentScriptedServer) scriptMembers(members []string) {
	s.mu.Lock()
	s.members = members
	s.mu.Unlock()
}

func (s *recentScriptedServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *recentScriptedServer) handle(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		cmd, err := parseRespCommand(r)
		if err != nil {
			return
		}
		if len(cmd.args) == 0 {
			continue
		}
		switch strings.ToUpper(cmd.args[0]) {
		case "HELLO":
			writeLine(c, "-ERR unknown command 'HELLO'\r\n")
		case "ZRANGEBYSCORE":
			s.writeMemberArray(c, cmd.args)
		default:
			writeLine(c, ":1\r\n")
		}
	}
}

// writeLine emits one raw RESP line; a write failure ends the connection.
func writeLine(c net.Conn, line string) {
	if _, err := io.WriteString(c, line); err != nil {
		c.Close()
	}
}

// writeMemberArray answers a ZRANGEBYSCORE with the currently scripted
// members, recording the command that asked so tests can pin the read shape.
func (s *recentScriptedServer) writeMemberArray(c net.Conn, args []string) {
	s.mu.Lock()
	members := append([]string(nil), s.members...)
	s.lastRange = append([]string(nil), args...)
	s.mu.Unlock()

	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(members))
	for _, m := range members {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(m), m)
	}
	writeLine(c, b.String())
}

func dialRecentClient(tb testing.TB, addr string) valkey.Client {
	tb.Helper()
	real, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{addr},
		AlwaysRESP2:       true,
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(tb, err)
	tb.Cleanup(real.Close)
	return real
}

// recentRecordingClient captures every pipelined command while delegating to
// the real client, so writes both land on the scripted server and are
// assertable as raw argument vectors.
type recentRecordingClient struct {
	valkey.Client

	mu   sync.Mutex
	cmds [][]string
}

func (r *recentRecordingClient) DoMulti(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	r.mu.Lock()
	for _, cmd := range cmds {
		// Deep-copy before delegating: execution returns the builder state to
		// valkey-go's pool, which zeroes whatever Commands() aliased.
		r.cmds = append(r.cmds, append([]string(nil), cmd.Commands()...))
	}
	r.mu.Unlock()
	return r.Client.DoMulti(ctx, cmds...)
}

func (r *recentRecordingClient) captured() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.cmds...)
}

func newRecentStoreUnderTest(tb testing.TB) (*ValkeyRecent, *recentRecordingClient, *recentScriptedServer) {
	tb.Helper()
	server := newRecentScriptedServer(tb)
	rec := &recentRecordingClient{Client: dialRecentClient(tb, server.ln.Addr().String())}
	v := NewValkeyRecent(rec, zap.NewNop())
	return v, rec, server
}

// --- flush shape ---

func TestValkeyRecentFlushPipelinesPerChannelShape(t *testing.T) {
	v, rec, _ := newRecentStoreUnderTest(t)

	v.Record(123, soloChatEnv("999", "hello there"), nukeClockBase)
	v.Record(123, cohortChatEnv([]string{"555", "556"}, "same copypasta everywhere"), nukeClockBase.Add(time.Second))
	v.Record(456, soloChatEnv("777", "other channel line"), nukeClockBase)
	v.flush(context.Background())

	cmds := rec.captured()

	byKey := map[string][][]string{}
	for _, cmd := range cmds {
		require.GreaterOrEqual(t, len(cmd), 2)
		byKey[cmd[1]] = append(byKey[cmd[1]], cmd)
	}
	require.Len(t, byKey, 2, "one pipelined group per touched channel")

	k123 := byKey["am:recent:123"]
	require.Len(t, k123, 4)
	assert.Equal(t, []string{"ZADD", "am:recent:123",
		strconv.FormatInt(nukeClockBase.UnixMilli(), 10), "999:0:hello there",
		strconv.FormatInt(nukeClockBase.Add(time.Second).UnixMilli(), 10), "555:0:same copypasta everywhere",
		strconv.FormatInt(nukeClockBase.Add(time.Second).UnixMilli(), 10), "556:0:same copypasta everywhere"}, k123[0])
	cutoff := strconv.FormatInt((nukeClockBase.Add(time.Second).UnixNano()-int64(recentTTL))/int64(time.Millisecond), 10)
	assert.Equal(t, []string{"ZREMRANGEBYSCORE", "am:recent:123", "-inf", cutoff}, k123[1])
	assert.Equal(t, []string{"ZREMRANGEBYRANK", "am:recent:123", "0", strconv.Itoa(-(recentRingCap + 1))}, k123[2])
	assert.Equal(t, []string{"EXPIRE", "am:recent:123", strconv.FormatInt(int64(recentTTL/time.Second), 10)}, k123[3])

	assert.Equal(t, "am:recent:456", byKey["am:recent:456"][0][1], "the second channel is tenant-scoped")
}

func TestValkeyRecentSkipsCommandShapesAndEmptyText(t *testing.T) {
	v, rec, _ := newRecentStoreUnderTest(t)

	v.Record(123, soloChatEnv("999", "!nuke spam"), nukeClockBase)
	v.Record(123, soloChatEnv("999", ""), nukeClockBase)
	v.flush(context.Background())
	assert.Empty(t, rec.captured(), "nothing retainable means no round trip")
}

func TestValkeyRecentFlushAfterEmptyFlushIsNoop(t *testing.T) {
	v, rec, _ := newRecentStoreUnderTest(t)
	v.flush(context.Background())
	assert.Empty(t, rec.captured())
}

// --- sweep over the wire ---

func TestValkeyRecentSweepParsesMatchesAndDedupes(t *testing.T) {
	v, rec, server := newRecentStoreUnderTest(t)
	server.scriptMembers([]string{
		"111:0:join my FREE N1TRO giveaway",
		encodeRecentMember(recentEntry{uid: 111, text: "free nitro again!!"}), // same user, second line
		"222:4:free nitro is my whole personality",                            // lead mod: parsed, not filtered here
		"garbage-without-colons",
		"zero:0:no uid",
	})

	hits := v.Sweep(context.Background(), 123, "free nitro", nukeClockBase)
	require.Len(t, hits, 2)
	assert.Equal(t, channelID(111), hits[0].UserID)
	assert.Equal(t, module.RoleEveryone, hits[0].Role)
	assert.Equal(t, channelID(222), hits[1].UserID)
	assert.Equal(t, module.RoleLeadModerator, hits[1].Role)

	cmds := rec.captured() // sweep rides Do (single), so nothing pipelined was captured
	assert.Empty(t, cmds)
}

func TestValkeyRecentSweepCutoffRidesTheCommand(t *testing.T) {
	v, _, server := newRecentStoreUnderTest(t)

	v.Sweep(context.Background(), 123, "phrase", nukeClockBase)

	server.mu.Lock()
	last := append([]string(nil), server.lastRange...)
	server.mu.Unlock()
	wantMin := strconv.FormatInt(nukeClockBase.Add(-recentTTL).UnixMilli(), 10)
	require.Len(t, last, 7)
	assert.Equal(t, []string{"ZRANGEBYSCORE", "am:recent:123", wantMin, "+inf", "LIMIT", "0", strconv.Itoa(recentFetchLimit)}, last)
}

// --- guards ---

func TestValkeyRecentNilClientDegradesSilently(t *testing.T) {
	v := NewValkeyRecent(nil, zap.NewNop())
	v.Record(123, soloChatEnv("999", "free nitro everyone"), nukeClockBase)
	assert.Empty(t, v.Sweep(context.Background(), 123, "free nitro", nukeClockBase))
}

func TestParseRecentMember(t *testing.T) {
	e, ok := parseRecentMember("44322889:2:KEKW KEKW :D")
	assert.True(t, ok)
	assert.Equal(t, uint64(44322889), e.uid)
	assert.Equal(t, module.RoleVIP, e.role)
	assert.Equal(t, "KEKW KEKW :D", e.text)

	for _, bad := range []string{
		"", "nocolons", "abc:0:text", "44322889:x:text", "0:0:text", "44322889:", "44322889",
	} {
		_, ok := parseRecentMember(bad)
		assert.False(t, ok, bad)
	}
}

// --- error visibility ---

func TestValkeyRecentErrorsSurfaceOncePerInterval(t *testing.T) {
	v, rec, _ := newRecentStoreUnderTest(t)

	failFirst := true
	rec.Client = failClient{Client: rec.Client, shouldFail: &failFirst}
	v.client = rec

	v.Record(123, soloChatEnv("999", "hello world again"), nukeClockBase)
	v.flush(context.Background())

	// The failed batch left nothing behind that a later healthy flush would
	// double-write: pending was drained either way.
	v.Record(123, soloChatEnv("998", "second line here"), nukeClockBase)
	failFirst = false
	v.flush(context.Background())

	found := false
	for _, cmd := range rec.captured() {
		if zaddCarrying(cmd, "second line") {
			found = true
		}
	}
	assert.True(t, found, "entries recorded after a failed flush still land")
}

// zaddCarrying reports whether a captured command vector is the pipelined
// ZADD whose final score-member pair embeds text.
func zaddCarrying(cmd []string, text string) bool {
	return cmd[0] == "ZADD" && len(cmd) > 3 && strings.Contains(cmd[len(cmd)-1], text)
}

// wroteMemberContaining reports whether any captured pipelined ZADD carried a
// member whose text contains substr.
func wroteMemberContaining(rec *recentRecordingClient, substr string) bool {
	for _, cmd := range rec.captured() {
		if zaddCarriesMember(cmd, substr) {
			return true
		}
	}
	return false
}

// zaddCarriesMember scans a captured ZADD's score/member pairs (everything
// after the key) for a member whose text contains substr.
func zaddCarriesMember(cmd []string, substr string) bool {
	if len(cmd) < 4 || cmd[0] != "ZADD" {
		return false
	}
	for i := 3; i < len(cmd); i += 2 {
		if strings.Contains(cmd[i], substr) {
			return true
		}
	}
	return false
}

type failClient struct {
	valkey.Client
	shouldFail *bool
}

func (c failClient) DoMulti(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	if *c.shouldFail {
		out := make([]valkey.ValkeyResult, len(cmds))
		for i := range cmds {
			out[i] = valkey.NewErrorResult(errors.New("valkey down"))
		}
		return out
	}
	return c.Client.DoMulti(ctx, cmds...)
}

// TestValkeyRecentSweepKeepsOneHitPerSenderInOrder pins the two properties the
// uidSet dedup has to preserve. One hit per sender: a copypasta wave is one
// spammer on many retained lines, and !nuke must time them out once. Order
// preserved: hits is still the storage precisely because a map has none, and
// the sweep answers in the order members came off the ZSET walk.
func TestValkeyRecentSweepKeepsOneHitPerSenderInOrder(t *testing.T) {
	v, _, server := newRecentStoreUnderTest(t)
	server.scriptMembers([]string{
		"111:0:free nitro, first",
		"222:0:free nitro too",
		"111:0:free nitro, again",
		"333:0:free nitro three",
		"222:0:free nitro once more",
		"111:0:free nitro, still",
	})

	hits := v.Sweep(context.Background(), 123, "free nitro", nukeClockBase)

	require.Len(t, hits, 3)
	got := make([]channelID, len(hits))
	for i := range hits {
		got[i] = hits[i].UserID
	}
	assert.Equal(t, []channelID{111, 222, 333}, got)
}
