package module

import (
	stdjson "encoding/json"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/event/lane"

	"github.com/bytedance/sonic"
)

// These benchmarks compare the OLD per-message techniques (encoding/json decode,
// strings.NewReplacer expansion, map+json body build) against the NEW ones
// (sonic into a pooled envelope, expandCommand into a pooled buffer, struct+sonic
// body) so the round-trip processing-stage win is measured, not asserted. Run:
//
//	go test ./app/worker/module/ -run x -bench 'BenchmarkRT' -benchmem

var rtEnvelope = []byte(`{"type":"channel.chat.message","lane":"standard",` +
	`"broadcaster_user_id":"123456789","broadcaster_user_login":"streamer",` +
	`"chatter_user_id":"987654321","chatter_user_login":"viewer",` +
	`"text":"!so @friend check them out","badges":[{"set_id":"moderator"},{"set_id":"subscriber"}]}`)

// --- envelope decode: stdlib (old) vs sonic+pool (new) ---

func BenchmarkRTDecodeStdlib(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var env lane.Envelope
		if err := stdjson.Unmarshal(rtEnvelope, &env); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRTDecodeSonicPooled(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		env := GetEnvelope()
		if err := sonic.Unmarshal(rtEnvelope, env); err != nil {
			b.Fatal(err)
		}
		PutEnvelope(env)
	}
}

// --- template expansion: strings.NewReplacer (old) vs expandCommand+pool (new) ---

const rtTemplate = "hey {user}, {sender} sent {args} to {touser}"

func BenchmarkRTExpandReplacer(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = strings.NewReplacer(
			"{user}", "viewer",
			"{sender}", "viewer",
			"{args}", "@friend check them out",
			"{touser}", "friend",
		).Replace(rtTemplate)
	}
}

func BenchmarkRTExpandPooled(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf := GetBuf()
		buf = expandCommand(buf, rtTemplate, "viewer", "viewer", "@friend check them out", "friend")
		_ = string(buf)
		PutBuf(buf)
	}
}

// --- chat body build: map+json (old chatReply) vs struct+sonic (new) ---

func BenchmarkRTBodyMapMarshal(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		body, _ := stdjson.Marshal(map[string]string{
			"broadcaster_id": "123456789",
			"message":        "hey friend welcome",
		})
		_ = body
	}
}

func BenchmarkRTBodyStructSonic(b *testing.B) {
	type chatBody struct {
		BroadcasterID string `json:"broadcaster_id"`
		Message       string `json:"message"`
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		body, _ := sonic.Marshal(chatBody{BroadcasterID: "123456789", Message: "hey friend welcome"})
		_ = body
	}
}
