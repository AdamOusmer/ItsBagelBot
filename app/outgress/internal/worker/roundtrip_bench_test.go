package worker

import (
	"testing"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

// oldWithSenderID reconstructs the pre-rewrite chat-body identity injection: it
// decoded the body into a map[string]codec.RawMessage and re-marshaled it on every
// chat send. Kept here only to benchmark against the new withField byte-splice.
func oldWithSenderID(body []byte, senderID string) []byte {
	m := map[string]codec.RawMessage{}
	if len(body) > 0 {
		if err := codec.Unmarshal(body, &m); err != nil {
			return body
		}
	}
	if _, ok := m["sender_id"]; !ok {
		if b, err := codec.Marshal(senderID); err == nil {
			m["sender_id"] = b
		}
	}
	out, err := codec.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

var rtEnvelope = []byte(`{"type":"chat","broadcaster_id":"123456789","sender_id":"555555","payload":{"broadcaster_id":"123456789","message":"hey friend welcome to the stream"}}`)

func BenchmarkRTEnvelopeStdlib(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var message outgress.Message
		if err := codec.Unmarshal(rtEnvelope, &message); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRTEnvelopeSonic(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var message outgress.Message
		if err := codec.Unmarshal(rtEnvelope, &message); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRTEnvelopeSonicNoCopy(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var message outgress.Message
		if err := decodeMessage(rtEnvelope, &message); err != nil {
			b.Fatal(err)
		}
	}
}

var rtChatBody = []byte(`{"broadcaster_id":"123456789","message":"hey friend welcome to the stream"}`)

// BenchmarkRTSenderOld vs BenchmarkRTSenderNew compares the old map decode +
// re-marshal against the new in-place byte splice, the cost paid on every single
// chat send leaving outgress.
func BenchmarkRTSenderOld(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = oldWithSenderID(rtChatBody, "555555")
	}
}

func BenchmarkRTSenderNew(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = withField(rtChatBody, "sender_id", "555555")
	}
}
