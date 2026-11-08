// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package protobuf

import (
	"bytes"
	"errors"
	"testing"

	"ItsBagelBot/pkg/protobuf/internal/testpb"
	"google.golang.org/protobuf/encoding/protowire"
	vendor "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func sample() *testpb.Packet {
	return &testpb.Packet{Sequence: ^uint64(0), Label: "bagel 🥯", Payload: []byte("payload"), Child: &testpb.Child{Value: "child"}}
}

func encode(t testing.TB, value Message) []byte {
	t.Helper()
	size, err := Size(value)
	if err != nil {
		t.Fatal(err)
	}
	dst := make([]byte, size)
	n, err := MarshalTo(dst, value)
	if err != nil {
		t.Fatal(err)
	}
	if n != size {
		t.Fatal("wrong wire length")
	}
	return dst[:n]
}

func TestInteroperableOwnedRoundTrip(t *testing.T) {
	want := sample()
	raw := encode(t, want)
	var standard testpb.Packet
	if err := vendor.Unmarshal(raw, &standard); err != nil {
		t.Fatal(err)
	}
	if !vendor.Equal(want, &standard) {
		t.Fatal("optimized marshal differs from standard protobuf")
	}
	input, err := vendor.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	receiver := testpb.PacketFromVTPool()
	defer receiver.ReturnToVTPool()
	if err = Unmarshal(input, receiver); err != nil {
		t.Fatal(err)
	}
	clear(input)
	if !vendor.Equal(want, receiver) {
		t.Fatal("safe receive retained aliases into caller buffer")
	}
}

func TestDecodeReplacesAndClearsOnFailure(t *testing.T) {
	receiver := testpb.PacketFromVTPool()
	defer receiver.ReturnToVTPool()
	for _, want := range []*testpb.Packet{sample(), {Sequence: 2}, {}, {Label: "next"}} {
		if err := Unmarshal(encode(t, want), receiver); err != nil {
			t.Fatal(err)
		}
		if !vendor.Equal(want, receiver) {
			t.Fatal("absent fields retained from previous message")
		}
	}
	partial := append(encode(t, sample()), 0x80)
	if err := Unmarshal(partial, receiver); err == nil {
		t.Fatal("accepted partial message")
	}
	if !vendor.Equal(receiver, &testpb.Packet{}) {
		t.Fatal("partial decode retained old account fields")
	}
}

func TestUnknownFieldOwnership(t *testing.T) {
	raw := protowire.AppendTag(nil, 100, protowire.BytesType)
	raw = protowire.AppendBytes(raw, []byte("unknown data"))
	receiver := testpb.PacketFromVTPool()
	defer receiver.ReturnToVTPool()
	if err := Unmarshal(raw, receiver); err != nil {
		t.Fatal(err)
	}
	if !HasUnknown(receiver) {
		t.Fatal("retained unknown field not reported")
	}
	unknown := bytes.Clone(receiver.ProtoReflect().GetUnknown())
	clear(raw)
	if len(unknown) == 0 || !bytes.Equal(unknown, receiver.ProtoReflect().GetUnknown()) {
		t.Fatal("unknown data aliased wire")
	}
	if err := Unmarshal(nil, receiver); err != nil {
		t.Fatal(err)
	}
	if len(receiver.ProtoReflect().GetUnknown()) != 0 {
		t.Fatal("unknown data survived reset")
	}
	if HasUnknown(receiver) || HasUnknown(&Empty{}) || HasUnknown(nil) {
		t.Fatal("empty message reported unknown fields")
	}
}

func TestBoundsAndUnsupportedValues(t *testing.T) {
	var nilPacket *testpb.Packet
	for _, value := range []any{nil, nilPacket, 42, &emptypb.Empty{}} {
		if _, err := Check(value); !errors.Is(err, ErrUnsupportedMessage) {
			t.Fatalf("accepted %T", value)
		}
	}
	if _, err := MarshalTo(nil, sample()); !errors.Is(err, ErrShortBuffer) {
		t.Fatal("short output accepted")
	}
	receiver := testpb.PacketFromVTPool()
	defer receiver.ReturnToVTPool()
	if err := Unmarshal(encode(t, sample()), receiver); err != nil {
		t.Fatal(err)
	}
	if err := Unmarshal(make([]byte, MaxMessageBytes+1), receiver); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatal("oversized receive accepted")
	}
	if !vendor.Equal(receiver, &testpb.Packet{}) {
		t.Fatal("oversized receive retained old account data")
	}
	if err := Unmarshal(nil, nilPacket); !errors.Is(err, ErrUnsupportedMessage) {
		t.Fatal("typed nil receive accepted")
	}
}

type broken struct {
	size, n                              int
	panicSize, panicMarshal, panicDecode bool
	resets                               int
}

func (b *broken) SizeVT() int {
	if b.panicSize {
		panic("size")
	}
	return b.size
}
func (b *broken) MarshalToSizedBufferVT([]byte) (int, error) {
	if b.panicMarshal {
		panic("marshal")
	}
	return b.n, nil
}
func (b *broken) UnmarshalVT([]byte) error {
	if b.panicDecode {
		panic("decode")
	}
	return nil
}
func (b *broken) ResetVT() { b.resets++ }

func TestBrokenImplementationsAreContained(t *testing.T) {
	for _, value := range []*broken{{size: -1}, {size: MaxMessageBytes + 1}, {panicSize: true}, {size: 1, n: 2}, {size: 1, panicMarshal: true}} {
		if _, err := MarshalTo(make([]byte, 8), value); err == nil {
			t.Fatal("accepted malformed generated implementation")
		}
	}
	value := &broken{panicDecode: true}
	if err := Unmarshal(nil, value); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("decode panic: %v", err)
	}
	if value.resets != 2 {
		t.Fatal("failed decode did not reset before and after")
	}
}

func TestEmptyWireValidation(t *testing.T) {
	raw := protowire.AppendTag(nil, 1, protowire.VarintType)
	raw = protowire.AppendVarint(raw, 42)
	if err := Unmarshal(raw, &Empty{}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{{0}, {0x80}, {0x0c}, {0x0a, 0x08, 0x01}} {
		if err := Unmarshal(raw, &Empty{}); err == nil {
			t.Fatal("malformed empty payload accepted")
		}
	}
	if n, err := MarshalTo(nil, &Empty{}); n != 0 || err != nil {
		t.Fatalf("empty: %d %v", n, err)
	}
}

func TestWarmAllocationBudgets(t *testing.T) {
	message := sample()
	dst := make([]byte, message.SizeVT())
	allocations := testing.AllocsPerRun(1000, func() {
		if _, err := MarshalTo(dst, message); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("reused marshal allocated %g times", allocations)
	}
	scalar := &testpb.Packet{Sequence: 123}
	wire := encode(t, scalar)
	receiver := &testpb.Packet{}
	allocations = testing.AllocsPerRun(1000, func() {
		if err := Unmarshal(wire, receiver); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("scalar decode allocated %g times", allocations)
	}
}

func BenchmarkMarshalTo(b *testing.B) {
	message := sample()
	dst := make([]byte, message.SizeVT())
	b.ReportAllocs()
	for b.Loop() {
		if _, err := MarshalTo(dst, message); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkUnmarshalOwned(b *testing.B) {
	wire := encode(b, sample())
	receiver := testpb.PacketFromVTPool()
	defer receiver.ReturnToVTPool()
	b.ReportAllocs()
	for b.Loop() {
		if err := Unmarshal(wire, receiver); err != nil {
			b.Fatal(err)
		}
	}
}
