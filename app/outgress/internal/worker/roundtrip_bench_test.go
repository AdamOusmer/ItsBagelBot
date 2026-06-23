package worker

import (
	"encoding/json"
	"testing"
)

// oldWithSenderID reconstructs the pre-rewrite chat-body identity injection: it
// decoded the body into a map[string]json.RawMessage and re-marshaled it on every
// chat send. Kept here only to benchmark against the new withField byte-splice.
func oldWithSenderID(body []byte, senderID string) []byte {
	m := map[string]json.RawMessage{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &m); err != nil {
			return body
		}
	}
	if _, ok := m["sender_id"]; !ok {
		if b, err := json.Marshal(senderID); err == nil {
			m["sender_id"] = b
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
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
