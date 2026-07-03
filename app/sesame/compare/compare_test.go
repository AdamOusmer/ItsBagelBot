package compare

import (
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// custom returns a fake-Valkey Reader holding one everyone-perm "so" command.
func custom(resp string) Reader {
	return Reader{Cmds: map[string]projection.Command{
		"so": {Name: "so", Response: resp, IsActive: true, Perm: "everyone"},
	}}
}

// TestOutputParity feeds identical envelopes to both pipelines and asserts they
// publish byte-identical outgress messages. One difference is intentional and
// covered separately (TestAnnounceModelDiffers): worker ships !announce* as baked
// commands, sesame treats announce as output middleware.
func TestOutputParity(t *testing.T) {
	shoutoutOn := Reader{Mods: []projection.ModuleView{{Name: "shoutout", IsEnabled: true}}}

	type scenario struct {
		name    string
		msg     *message.Message
		reader  Reader
		special string
		live    bool
		greet   bool
	}
	scenarios := []scenario{
		{name: "info itsbagelbot", msg: ChatMsg("!itsbagelbot", "9", "standard")},
		{name: "info source premium", msg: ChatMsg("!source", "9", "premium")},
		{name: "plain chat no output", msg: ChatMsg("just chatting here", "9", "standard")},
		{name: "custom plain", msg: ChatMsg("!so", "9", "standard"), reader: custom("hello {sender}")},
		{name: "custom announce", msg: ChatMsg("!so @bob hi there", "9", "premium"), reader: custom("/announce {user} says: {args}; to={target}")},
		{name: "custom announce blue", msg: ChatMsg("!so cold news", "9", "standard"), reader: custom("/announceblue {args}")},
		{name: "custom shoutout", msg: ChatMsg("!so @cooldude go", "9", "premium"), reader: custom("/shoutout @cooldude check them out")},
		{name: "custom announce empty dropped", msg: ChatMsg("!so", "9", "standard"), reader: custom("/announce")},
		{name: "bagel greet special first live", msg: ChatMsg("hi chat", "1", "premium"), special: "1", live: true, greet: true},
		{name: "bagel skipped offline", msg: ChatMsg("hi chat", "1", "standard"), special: "1", live: false, greet: true},
		{name: "raid shoutout default", msg: RaidMsg("standard"), reader: shoutoutOn},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			w, s := ProcessBoth(sc.msg, sc.reader, sc.special, sc.live, sc.greet)
			assert.Equal(t, w, s, "worker and sesame must publish identical outgress messages")
		})
	}
}

// TestPingParity checks !ping separately: the uptime string is time-dependent, so
// it asserts identical Type/BroadcasterID/subject and the same fixed prefix.
func TestPingParity(t *testing.T) {
	w, s := ProcessBoth(ChatMsg("!ping", "9", "standard"), Reader{}, "", false, false)
	require.Len(t, w, 1)
	require.Len(t, s, 1)
	assert.Equal(t, w[0].Subject, s[0].Subject)
	assert.Equal(t, w[0].Msg.Type, s[0].Msg.Type)
	assert.Equal(t, w[0].Msg.BroadcasterID, s[0].Msg.BroadcasterID)
	assert.True(t, strings.Contains(chatText(t, w[0].Msg), "up for"))
	assert.True(t, strings.Contains(chatText(t, s[0].Msg), "up for"))
}

// TestAnnounceModelDiffers documents the one intentional divergence.
func TestAnnounceModelDiffers(t *testing.T) {
	msg := ChatMsg("!announce hello", "9", "standard", "moderator")

	wpub := &Pub{}
	NewWorker(wpub, Reader{}, "", Live{}, Greet{}).Process(msg)
	require.Len(t, wpub.Got, 1, "worker still ships !announce as a baked command")
	assert.Equal(t, outgress.TypeAnnounce, wpub.Got[0].Msg.Type)

	spub := &Pub{}
	NewSesame(spub, Reader{}, "", Live{}, Greet{}).Process(msg)
	assert.Empty(t, spub.Got, "sesame has no baked !announce; announce is middleware over a custom command")
}

func chatText(t *testing.T, m outgress.Message) string {
	t.Helper()
	var inner struct {
		Message string `json:"message"`
	}
	require.NoError(t, sonic.Unmarshal(m.Payload, &inner))
	return inner.Message
}

// --- timing, side by side ---

func benchProcess(b *testing.B, p Processor, msg *message.Message) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.Process(msg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWorkerNoOutput(b *testing.B) {
	benchProcess(b, NewWorker(&Pub{}, Reader{}, "", Live{}, Greet{}), ChatMsg("hello chat how is everyone", "9", "standard"))
}
func BenchmarkSesameNoOutput(b *testing.B) {
	benchProcess(b, NewSesame(&Pub{}, Reader{}, "", Live{}, Greet{}), ChatMsg("hello chat how is everyone", "9", "standard"))
}
func BenchmarkWorkerInfoCommand(b *testing.B) {
	benchProcess(b, NewWorker(&Pub{}, Reader{}, "", Live{}, Greet{}), ChatMsg("!source", "9", "standard"))
}
func BenchmarkSesameInfoCommand(b *testing.B) {
	benchProcess(b, NewSesame(&Pub{}, Reader{}, "", Live{}, Greet{}), ChatMsg("!source", "9", "standard"))
}
func BenchmarkWorkerCustomAnnounce(b *testing.B) {
	benchProcess(b, NewWorker(&Pub{}, custom("/announce {user}: {args}"), "", Live{}, Greet{}), ChatMsg("!so hi there", "9", "standard"))
}
func BenchmarkSesameCustomAnnounce(b *testing.B) {
	benchProcess(b, NewSesame(&Pub{}, custom("/announce {user}: {args}"), "", Live{}, Greet{}), ChatMsg("!so hi there", "9", "standard"))
}
