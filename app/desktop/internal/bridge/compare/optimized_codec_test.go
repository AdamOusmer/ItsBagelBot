// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package compare

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestVTCodecEquivalence(t *testing.T) {
	for _, name := range []string{"chat", "chat_large", "alert"} {
		t.Run(name, func(t *testing.T) {
			want := SampleEvent(name)
			raw, err := want.MarshalVT()
			if err != nil {
				t.Fatal(err)
			}
			if len(raw) != want.SizeVT() {
				t.Fatal("VT size differs from serialized size")
			}
			var standard Event
			if err = proto.Unmarshal(raw, &standard); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(want, &standard) {
				t.Fatal("VT encoder differs from standard decoder")
			}
			raw, err = proto.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			var generated Event
			if err = generated.UnmarshalVT(raw); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(want, &generated) {
				t.Fatal("VT decoder differs from standard encoder")
			}
			assertVTBufferRoundtrip(t, want)
		})
	}
}

func assertVTBufferRoundtrip(t *testing.T, want *Event) {
	t.Helper()
	buffer := make([]byte, want.SizeVT())
	n, err := want.MarshalToSizedBufferVT(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(buffer) {
		t.Fatal("VT did not fill exact buffer")
	}
	var standard Event
	if err = proto.Unmarshal(buffer, &standard); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(want, &standard) {
		t.Fatal("buffer VT encoder differs from standard decoder")
	}
}

func TestVTPooledDecodeClearsAbsentFields(t *testing.T) {
	// Both the outer message and nested messages are VT-pool owned. Every decode
	// starts empty; UnmarshalVT alone has merge semantics and is insufficient.
	pooled := EventFromVTPool()
	defer pooled.ReturnToVTPool()
	sequence := []*Event{SampleEvent("chat"), SampleEvent("alert"), {}, SampleEvent("chat_large"), SampleEvent("alert"), {}}
	for _, want := range sequence {
		raw, err := proto.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		pooled.ResetVT()
		if err = pooled.UnmarshalVT(raw); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(want, pooled) {
			t.Fatal("pool reuse retained an absent message or scalar field")
		}
	}
}

func TestVTSafeDecodeOwnsStrings(t *testing.T) {
	want := SampleEvent("chat")
	raw, err := proto.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	decoded := EventFromVTPool()
	defer decoded.ReturnToVTPool()
	if err = decoded.UnmarshalVT(raw); err != nil {
		t.Fatal(err)
	}
	clear(raw)
	if !proto.Equal(want, decoded) {
		t.Fatal("safe VT decode retained aliases into receive buffer")
	}
}

func TestVTResetClearsUnknownFields(t *testing.T) {
	base := SampleEvent("alert")
	raw, err := proto.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	raw = protowire.AppendTag(raw, 100, protowire.BytesType)
	raw = protowire.AppendBytes(raw, []byte("unknown"))
	pooled := EventFromVTPool()
	defer pooled.ReturnToVTPool()
	if err = pooled.UnmarshalVT(raw); err != nil {
		t.Fatal(err)
	}
	unknown := bytes.Clone(pooled.ProtoReflect().GetUnknown())
	if len(unknown) == 0 {
		t.Fatal("fixture did not retain unknown fields")
	}
	clear(raw)
	if !bytes.Equal(pooled.ProtoReflect().GetUnknown(), unknown) {
		t.Fatal("unknown fields alias receive buffer")
	}
	pooled.ResetVT()
	plain, err := proto.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if err = pooled.UnmarshalVT(plain); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(base, pooled) {
		t.Fatal("old unknown fields survived pooled reset")
	}
}

func BenchmarkOptimizedEncode(b *testing.B) {
	for _, name := range []string{"chat", "chat_large", "alert"} {
		b.Run(name, func(b *testing.B) {
			event := SampleEvent(name)
			b.Run("vt_owned", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					data, err := event.MarshalVT()
					if err != nil {
						b.Fatal(err)
					}
					if len(data) == 0 {
						b.Fatal("empty output")
					}
				}
			})
			b.Run("vt_reuse", func(b *testing.B) {
				data := make([]byte, event.SizeVT())
				b.ReportAllocs()
				for b.Loop() {
					n, err := event.MarshalToSizedBufferVT(data)
					if err != nil {
						b.Fatal(err)
					}
					if n != len(data) {
						b.Fatal("wrong output size")
					}
				}
			})
		})
	}
}

func BenchmarkOptimizedDecode(b *testing.B) {
	for _, name := range []string{"chat", "chat_large", "alert"} {
		b.Run(name, func(b *testing.B) {
			data, err := proto.Marshal(SampleEvent(name))
			if err != nil {
				b.Fatal(err)
			}
			b.Run("vt_reset", func(b *testing.B) {
				var out Event
				b.SetBytes(int64(len(data)))
				b.ReportAllocs()
				for b.Loop() {
					out.Reset()
					if err := out.UnmarshalVT(data); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("vt_pool", func(b *testing.B) {
				out := EventFromVTPool()
				defer out.ReturnToVTPool()
				if err := out.UnmarshalVT(data); err != nil {
					b.Fatal(err)
				}
				b.SetBytes(int64(len(data)))
				b.ReportAllocs()
				for b.Loop() {
					out.ResetVT()
					if err := out.UnmarshalVT(data); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
