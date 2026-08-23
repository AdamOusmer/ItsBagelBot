// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/automod"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
)

// fakeCampaign returns a fixed distinct-sender count and records how often it
// was consulted, with which tenant and sender.
type fakeCampaign struct {
	mu    sync.Mutex
	count int
	calls int
	seen  []campaignCall
}

// campaignCall is one recorded Observe invocation.
type campaignCall struct {
	broadcasterID uint64
	simhash       uint64
	sender        string
}

func (c *fakeCampaign) Observe(_ context.Context, broadcasterID uint64, simhash uint64, senderID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.seen = append(c.seen, campaignCall{broadcasterID: broadcasterID, simhash: simhash, sender: senderID})
	return c.count
}

func councilPipeline(pub *fakePublisher, camp Campaign) *Pipeline {
	// Loaded-empty emote set: keeps the caps heuristic enforcing. A never-loaded
	// gate now suppresses caps-only lines by design (automod's
	// TestCapsOnlyRescueByEmoteAvailability), which would silently turn this
	// suite's all-caps fixture into a no-op instead of a delete to escalate.
	gate := automod.New()
	gate.SetEmotes(automod.NewEmoteSet(nil))
	d := Deps{
		Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(), Automod: gate, Campaign: camp,
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutomodEnforce: true,
	})
}

// linkChat is a clean, link-bearing chat line (no content verdict on its own)
// with a message id so deletes can be exercised.
func linkChat(t *testing.T, chatter string) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     chatter,
		"msg_id":              "m-" + chatter,
		"text":                "hey friends come check this great video at https://example.com/watch tonight",
	})
	require.NoError(t, err)
	return bus.NewMessage("u-"+chatter, body)
}

func TestCampaignEscalatesLinkFloodToDelete(t *testing.T) {
	camp := &fakeCampaign{count: campaignThreshold}
	pub := &fakePublisher{}
	p := councilPipeline(pub, camp)

	require.NoError(t, p.Process(linkChat(t, "777")))
	require.Len(t, pub.got, 1, "campaign corroboration adds the mildest action")
	assert.Equal(t, outgress.TypeDelete, pub.got[0].msg.Type)
	assert.Equal(t, "m-777", pub.got[0].msg.MsgID)
	assert.Equal(t, 1, camp.calls)
}

func TestCampaignBelowThresholdDoesNothing(t *testing.T) {
	camp := &fakeCampaign{count: campaignThreshold - 1}
	pub := &fakePublisher{}
	p := councilPipeline(pub, camp)

	require.NoError(t, p.Process(linkChat(t, "777")))
	assert.Empty(t, pub.got, "below the quorum the campaign juror abstains")
	assert.Equal(t, 1, camp.calls, "the line was still counted")
}

func TestCampaignNotConsultedOnCleanShortChat(t *testing.T) {
	camp := &fakeCampaign{count: 100}
	pub := &fakePublisher{}
	p := councilPipeline(pub, camp)

	require.NoError(t, p.Process(chatMsg(t, "standard", "nice play")))
	assert.Empty(t, pub.got)
	assert.Zero(t, camp.calls, "a clean short line never reaches the campaign juror")
}

func TestCampaignEscalatesFlaggedDeleteToTimeout(t *testing.T) {
	camp := &fakeCampaign{count: campaignThreshold}
	pub := &fakePublisher{}
	p := councilPipeline(pub, camp)

	// A caps heuristic line (delete verdict) corroborated by the campaign juror
	// becomes a timeout.
	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     "888",
		"msg_id":              "m-888",
		"text":                "FREE VBUCKS CLICK MY PROFILE RIGHT NOW EVERYONE HURRY",
	})
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage("u", body)))

	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type, "delete + campaign quorum = timeout")
}

func TestHarassmentWarnPairsWithDelete(t *testing.T) {
	pub := &fakePublisher{}
	p := councilPipeline(pub, nil)

	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     "999",
		"msg_id":              "m-999",
		"text":                "nobody asked just go kill yourself already dude seriously",
	})
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage("u", body)))

	got := countByType(pub.got)
	assert.Equal(t, 1, got[outgress.TypeWarn], "harassment issues a formal warning")
	assert.Equal(t, 1, got[outgress.TypeDelete], "and removes the message")
}

func TestWarnLadderEscalatesByReputation(t *testing.T) {
	warn := automod.Verdict{Action: automod.ActionWarn, Rule: "lex:harassment:x"}
	v := escalateByReputation(warn, repWarnToTimeoutScore)
	assert.Equal(t, automod.ActionTimeout, v.Action, "a repeat offender's warn becomes a timeout")
	assert.EqualValues(t, 600, v.Seconds)

	v = escalateByReputation(warn, 0)
	assert.Equal(t, automod.ActionWarn, v.Action, "first strike stays a warn")

	timeout := automod.Verdict{Action: automod.ActionTimeout, Seconds: 600}
	assert.Equal(t, automod.ActionBan, escalateByReputation(timeout, repEscalateThreshold).Action)
}

func TestBuildOutgressDeleteAndWarn(t *testing.T) {
	body, err := buildOutgress(&module.Output{
		Type:          outgress.TypeDelete,
		BroadcasterID: "77",
		MsgID:         "abc-123",
	})
	require.NoError(t, err)
	var msg outgress.Message
	require.NoError(t, codec.Unmarshal(body, &msg))
	assert.Equal(t, outgress.TypeDelete, msg.Type)
	assert.Equal(t, "abc-123", msg.MsgID)
	// A nil RawMessage marshals as JSON null: no body either way.
	assert.True(t, len(msg.Payload) == 0 || string(msg.Payload) == "null", "delete has no body")

	body, err = buildOutgress(&module.Output{
		Type:          outgress.TypeWarn,
		BroadcasterID: "77",
		TargetUserID:  "999",
		Reason:        "automod:lex:harassment:x",
	})
	require.NoError(t, err)
	require.NoError(t, codec.Unmarshal(body, &msg))
	assert.Equal(t, outgress.TypeWarn, msg.Type)
	var wire banBodyWire
	require.NoError(t, codec.Unmarshal(msg.Payload, &wire))
	assert.Equal(t, "999", wire.Data.UserID)
	assert.Equal(t, "automod:lex:harassment:x", wire.Data.Reason)
}

// The campaign juror must never fire on ordinary chat volume: distinct chatters
// posting DIFFERENT clean lines share no template, and the fake here proves the
// pipeline only consults the juror for link-bearing or already-flagged lines.
func TestOrdinaryChatterFlowNeverCounts(t *testing.T) {
	camp := &fakeCampaign{count: 100}
	pub := &fakePublisher{}
	p := councilPipeline(pub, camp)

	for i := 0; i < 5; i++ {
		require.NoError(t, p.Process(chatMsg(t, "standard", "that jungle gank was so clean honestly round "+strconv.Itoa(i))))
	}
	assert.Zero(t, camp.calls)
	assert.Empty(t, pub.got)
}

// The juror must be consulted with the broadcaster id of the moderation
// context the line arrived in, so quorums can never fuse across tenants.
func TestCampaignJurorReceivesTenantScope(t *testing.T) {
	tests := []struct {
		name          string
		broadcasterID string
		chatter       string
	}{
		{name: "first channel", broadcasterID: "123", chatter: "777"},
		{name: "second channel", broadcasterID: "456", chatter: "778"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			camp := &fakeCampaign{}
			d := Deps{
				Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
				Pub: &fakePublisher{}, Log: zap.NewNop(), Automod: automod.New(), Campaign: camp,
			}
			p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{
				OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutomodEnforce: true,
			})
			body, err := codec.Marshal(map[string]any{
				"type":                chatType,
				"lane":                "standard",
				"broadcaster_user_id": tc.broadcasterID,
				"chatter_user_id":     tc.chatter,
				"msg_id":              "m-" + tc.chatter,
				"text":                "hey friends come check this great video at https://example.com/watch tonight",
			})
			require.NoError(t, err)

			require.NoError(t, p.Process(bus.NewMessage("u-"+tc.chatter, body)))
			require.NotZero(t, camp.calls, "the link-bearing line reached the juror")
			want, err := strconv.ParseUint(tc.broadcasterID, 10, 64)
			require.NoError(t, err)
			assert.Equal(t, want, camp.seen[0].broadcasterID)
			assert.Equal(t, tc.chatter, camp.seen[0].sender)
		})
	}
}

// ---- offline valkey test double -------------------------------------------
//
// ValkeyCampaign needs a real command Builder, which only a live valkey.Client
// hands out (the builder constructors are internal to the library), and
// NewClient dials eagerly. The listener below accepts the connection and
// answers every init handshake command with the one error message the client's
// RESP2 fallback tolerates ("unknown command 'HELLO'"), so a real Client — and
// with it B() — exists without a server. The recording client then intercepts
// DoMulti, capturing built commands and handing back scripted replies.
type dumbValkeyServer struct{}

func newFakeValkey(tb testing.TB) *recordingValkey {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(tb, err)
	tb.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					if _, err := readRespCommand(r); err != nil {
						return
					}
					if _, err := c.Write([]byte("-ERR unknown command 'HELLO'\r\n")); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	real, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{ln.Addr().String()},
		AlwaysRESP2:       true,
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(tb, err)
	tb.Cleanup(real.Close)
	return &recordingValkey{Client: real}
}

// recordingValkey captures every DoMulti command and replays scripted replies
// positionally (unscripted positions succeed with an empty message).
type recordingValkey struct {
	valkey.Client

	mu    sync.Mutex
	cmds  [][]string
	resps []valkey.ValkeyResult
}

func (f *recordingValkey) DoMulti(_ context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]valkey.ValkeyResult, len(cmds))
	for i, cmd := range cmds {
		f.cmds = append(f.cmds, cmd.Commands())
		switch {
		case i < len(f.resps):
			out[i] = f.resps[i]
		default:
			out[i] = valkey.NewResult(valkey.ValkeyMessage{}, nil)
		}
	}
	return out
}

func (f *recordingValkey) script(resps ...valkey.ValkeyResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resps = resps
}

func (f *recordingValkey) captured() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.cmds...)
}

// readRespCommand consumes one RESP array-of-bulk-strings command.
func readRespCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if line[0] != '*' {
		if strings.TrimSpace(line) == "" {
			return nil, nil
		}
		return []string{line}, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hdr, err := r.ReadString('\n')
		if err != nil || hdr[0] != '$' {
			return nil, errors.New("bad bulk header")
		}
		ln, err := strconv.Atoi(strings.TrimSpace(hdr[1:]))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, ln+2) // payload + CRLF
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:ln]))
	}
	return args, nil
}

// The keys Observe writes must carry the broadcaster id between the prefix and
// the band hex, in PFADD k1, PFADD k2, EXPIRE k1, EXPIRE k2, PFCOUNT k1,
// PFCOUNT k2 order, with the sliding window TTL on both bands.
func TestValkeyCampaignKeyScoping(t *testing.T) {
	const simhash = 0x0123456789abcdef // bands: 1234567 / 89abcdef
	tests := []struct {
		name          string
		broadcasterID uint64
		sender        string
		wantKeys      [6]string
	}{
		{
			name:          "tenant 123",
			broadcasterID: 123,
			sender:        "777",
			wantKeys: [6]string{
				"am:tmpl:123:1234567", "am:tmpl:123:89abcdef",
				"am:tmpl:123:1234567", "am:tmpl:123:89abcdef",
				"am:tmpl:123:1234567", "am:tmpl:123:89abcdef",
			},
		},
		{
			name:          "tenant 987654321 same template stays isolated",
			broadcasterID: 987654321,
			sender:        "888",
			wantKeys: [6]string{
				"am:tmpl:987654321:1234567", "am:tmpl:987654321:89abcdef",
				"am:tmpl:987654321:1234567", "am:tmpl:987654321:89abcdef",
				"am:tmpl:987654321:1234567", "am:tmpl:987654321:89abcdef",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fk := newFakeValkey(t)
			c := NewValkeyCampaign(fk, zaptest.NewLogger(t))

			c.Observe(context.Background(), tc.broadcasterID, simhash, tc.sender)

			ttl := strconv.FormatInt(int64(campaignWindow.Seconds()), 10)
			want := [][]string{
				{"PFADD", tc.wantKeys[0], tc.sender},
				{"PFADD", tc.wantKeys[1], tc.sender},
				{"EXPIRE", tc.wantKeys[2], ttl},
				{"EXPIRE", tc.wantKeys[3], ttl},
				{"PFCOUNT", tc.wantKeys[4]},
				{"PFCOUNT", tc.wantKeys[5]},
			}
			assert.Equal(t, want, fk.captured())
		})
	}
}

// Two tenants observing the identical template must not share a single key:
// the old fleet-wide am:tmpl:<band> scheme fused their quorums. (Keys repeat
// within one Observe — pfadd/expire/pfcount on each band — so the invariant is
// disjointness across tenants, not global uniqueness.)
func TestValkeyCampaignTenantsShareNoKeys(t *testing.T) {
	fk := newFakeValkey(t)
	c := NewValkeyCampaign(fk, zaptest.NewLogger(t))

	const simhash = 0x0123456789abcdef
	c.Observe(context.Background(), 123, simhash, "777")
	c.Observe(context.Background(), 456, simhash, "888")

	keys := map[string]map[uint64]bool{}
	for _, cmd := range fk.captured() {
		idStr, _, ok := strings.Cut(strings.TrimPrefix(cmd[1], "am:tmpl:"), ":")
		if !ok {
			t.Fatalf("command %q carries an unscoped key", cmd)
		}
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			t.Fatalf("command %q carries a malformed tenant scope", cmd)
		}
		if keys[cmd[1]] == nil {
			keys[cmd[1]] = map[uint64]bool{}
		}
		keys[cmd[1]][id] = true
	}
	for key, tenants := range keys {
		assert.Lenf(t, tenants, 1, "key %q is shared across tenants %v", key, tenants)
	}
}

// Guard clauses must short-circuit before any client call: the nil client
// would panic on the first built command if an observation slipped through.
func TestValkeyCampaignGuardsShortCircuit(t *testing.T) {
	c := NewValkeyCampaign(nil, zaptest.NewLogger(t))

	assert.Zero(t, c.Observe(context.Background(), 0, 0x1234, "777"), "no broadcaster")
	assert.Zero(t, c.Observe(context.Background(), 123, 0, "777"), "no simhash")
	assert.Zero(t, c.Observe(context.Background(), 123, 0x1234, ""), "no sender")
}

// PFADD/EXPIRE failures must surface (once per interval, carrying how many
// were swallowed) instead of vanishing — but never change the returned count.
func TestCampaignWriteErrorsVisibleOncePerInterval(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	c := &ValkeyCampaign{log: zap.New(core)}
	boom := errors.New("valkey down")

	bad := []valkey.ValkeyResult{
		valkey.NewErrorResult(boom),
		valkey.NewResult(valkey.ValkeyMessage{}, nil),
		valkey.NewErrorResult(boom),
		valkey.NewResult(valkey.ValkeyMessage{}, nil),
	}

	c.noteWriteErrors(bad)
	assert.Equal(t, int64(0), c.errPending.Load(), "counter resets after logging")
	require.Len(t, logs.All(), 1, "first failure in an interval logs immediately")
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, int64(2), fields["suppressed"])
	assert.Contains(t, logs.All()[0].Message, "write failed")

	// A second sweep inside the interval is counted but not logged.
	c.noteWriteErrors(bad)
	assert.Equal(t, int64(2), c.errPending.Load())
	assert.Len(t, logs.All(), 1)

	// Once the interval lapses, the next failure logs the total swallowed:
	// the two errors from the suppressed sweep plus its own.
	c.lastWriteLogNs.Add(-int64(2 * campaignErrLogInterval))
	c.noteWriteErrors(bad)
	require.Len(t, logs.All(), 2)
	assert.Equal(t, int64(4), logs.All()[1].ContextMap()["suppressed"])

	// Success replies never log and never count.
	c.lastWriteLogNs.Store(time.Now().Add(-2 * campaignErrLogInterval).UnixNano())
	good := []valkey.ValkeyResult{
		valkey.NewResult(valkey.ValkeyMessage{}, nil),
		valkey.NewResult(valkey.ValkeyMessage{}, nil),
		valkey.NewResult(valkey.ValkeyMessage{}, nil),
		valkey.NewResult(valkey.ValkeyMessage{}, nil),
	}
	c.noteWriteErrors(good)
	assert.Len(t, logs.All(), 2)
	assert.Equal(t, int64(0), c.errPending.Load())

	// And the counting read path stays fail-open over scripted write errors.
	fk := newFakeValkey(t)
	fk.script(bad...)
	vc := NewValkeyCampaign(fk, zaptest.NewLogger(t))
	assert.Zero(t, vc.Observe(context.Background(), 123, 0x0123456789abcdef, "777"))
}
