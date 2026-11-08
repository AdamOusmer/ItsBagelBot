// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package grpc

import (
	"bytes"
	"errors"
	"testing"

	"ItsBagelBot/pkg/grpc/internal/testpb"

	"google.golang.org/grpc/encoding"
	_ "google.golang.org/grpc/encoding/proto"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

var _ encoding.CodecV2 = vtCodec{}

func samplePacket() *testpb.Packet {
	return &testpb.Packet{Sequence: ^uint64(0), Label: "bagel 🥯", Child: &testpb.Child{Value: "child"}, Payload: []byte("payload")}
}

func TestCodecInteroperability(t *testing.T) {
	want := samplePacket()
	encoded, err := (vtCodec{}).Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	defer encoded.Free()
	var standard testpb.Packet
	if err = proto.Unmarshal(encoded.Materialize(), &standard); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(want, &standard) {
		t.Fatal("optimized encoding differs from standard protobuf")
	}
	raw, err := proto.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	decoded := testpb.PacketFromVTPool()
	defer decoded.ReturnToVTPool()
	if err = (vtCodec{}).Unmarshal(mem.BufferSlice{mem.SliceBuffer(raw[:3]), mem.SliceBuffer(raw[3:])}, decoded); err != nil {
		t.Fatal(err)
	}
	clear(raw)
	if !proto.Equal(want, decoded) {
		t.Fatal("safe codec retained aliases into fragmented wire buffer")
	}
}

func TestCodecResetsMixedMessages(t *testing.T) {
	receiver := testpb.PacketFromVTPool()
	defer receiver.ReturnToVTPool()
	for _, want := range []*testpb.Packet{samplePacket(), {Label: "different"}, {}, samplePacket(), {Sequence: 2}} {
		raw, err := proto.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		if err = (vtCodec{}).Unmarshal(mem.BufferSlice{mem.SliceBuffer(raw)}, receiver); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(want, receiver) {
			t.Fatal("old absent fields survived receive")
		}
	}
}

func TestFailedDecodeClearsDestination(t *testing.T) {
	receiver := testpb.PacketFromVTPool()
	defer receiver.ReturnToVTPool()
	good, err := proto.Marshal(samplePacket())
	if err != nil {
		t.Fatal(err)
	}
	bad := append(bytes.Clone(good), 0x80)
	if err = (vtCodec{}).Unmarshal(mem.BufferSlice{mem.SliceBuffer(bad)}, receiver); err == nil {
		t.Fatal("accepted malformed protobuf")
	}
	if !proto.Equal(receiver, &testpb.Packet{}) {
		t.Fatal("failed decode retained partially decoded account data")
	}
	if err = (vtCodec{}).Unmarshal(mem.BufferSlice{nil}, receiver); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("nil buffer: %v", err)
	}
	if err = (vtCodec{}).Unmarshal(mem.BufferSlice{mem.SliceBuffer(make([]byte, maxCodecBytes+1))}, receiver); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("oversized buffer: %v", err)
	}
}

func TestEmptyValidatesUnknownFields(t *testing.T) {
	raw := protowire.AppendTag(nil, 42, protowire.BytesType)
	raw = protowire.AppendBytes(raw, []byte("valid unknown field"))
	if err := (&Empty{}).UnmarshalVT(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{{0}, {0x80}, {0x0c}, {0x0a, 0x08, 0x01}} {
		if err := (&Empty{}).UnmarshalVT(bad); err == nil {
			t.Fatalf("accepted malformed empty wire %x", bad)
		}
	}
	data, err := (vtCodec{}).Marshal(&Empty{})
	if err != nil || len(data) != 0 {
		t.Fatalf("empty marshal: %v %v", data, err)
	}
	if err = (vtCodec{}).Unmarshal(nil, &Empty{}); err != nil {
		t.Fatal(err)
	}
	// Keep wire compatibility while restricting the public API to optimized types.
	encoded, err := proto.Marshal(&emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if err = (vtCodec{}).Unmarshal(mem.BufferSlice{mem.SliceBuffer(encoded)}, &Empty{}); err != nil {
		t.Fatal(err)
	}
}

func TestNoGlobalCodecOverride(t *testing.T) {
	if _, ok := encoding.GetCodecV2("proto").(vtCodec); ok {
		t.Fatal("optimized codec globally registered")
	}
}

func TestUnsupportedMessages(t *testing.T) {
	var nilPacket *testpb.Packet
	for _, value := range []any{nil, nilPacket, 42, &emptypb.Empty{}} {
		if _, err := (vtCodec{}).Marshal(value); !errors.Is(err, ErrUnsupportedMessage) {
			t.Fatalf("marshal %T: %v", value, err)
		}
		if err := (vtCodec{}).Unmarshal(nil, value); !errors.Is(err, ErrUnsupportedMessage) {
			t.Fatalf("unmarshal %T: %v", value, err)
		}
	}
}

type badMessage struct {
	size         int
	count        int
	marshalPanic bool
	decodePanic  bool
}

func (m *badMessage) SizeVT() int { return m.size }
func (m *badMessage) MarshalToSizedBufferVT([]byte) (int, error) {
	if m.marshalPanic {
		panic("broken generated codec")
	}
	return m.count, nil
}
func (m *badMessage) UnmarshalVT([]byte) error {
	if m.decodePanic {
		panic("broken generated codec")
	}
	return nil
}
func (*badMessage) ResetVT() {}

func TestBadMessageImplementationsAreContained(t *testing.T) {
	for _, message := range []*badMessage{{size: -1}, {size: maxCodecBytes + 1}, {size: 1, count: 2}, {size: 1, marshalPanic: true}} {
		if _, err := (vtCodec{}).Marshal(message); err == nil {
			t.Fatal("accepted invalid optimized marshaler")
		}
	}
	if err := (vtCodec{}).Unmarshal(nil, &badMessage{decodePanic: true}); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("unmarshal panic escaped codec: %v", err)
	}
}

type panicSizer struct{ Empty }

func (*panicSizer) SizeVT() int { panic("broken size") }

func TestRequestPreflightContainsPanics(t *testing.T) {
	if _, err := messageSize(&panicSizer{}); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("preflight size panic: %v", err)
	}
	if _, err := (vtCodec{}).Marshal(&panicSizer{}); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("marshal size panic: %v", err)
	}
}

func BenchmarkCodecMarshal(b *testing.B) {
	message := samplePacket()
	b.ReportAllocs()
	for b.Loop() {
		data, err := (vtCodec{}).Marshal(message)
		if err != nil {
			b.Fatal(err)
		}
		data.Free()
	}
}

func BenchmarkCodecUnmarshal(b *testing.B) {
	raw, err := proto.Marshal(samplePacket())
	if err != nil {
		b.Fatal(err)
	}
	receiver := testpb.PacketFromVTPool()
	defer receiver.ReturnToVTPool()
	data := mem.BufferSlice{mem.SliceBuffer(raw)}
	b.ReportAllocs()
	for b.Loop() {
		if err := (vtCodec{}).Unmarshal(data, receiver); err != nil {
			b.Fatal(err)
		}
	}
}

func TestEmptyCodecAllocationBudget(t *testing.T) {
	message := &Empty{}
	allocations := testing.AllocsPerRun(1000, func() {
		if _, err := (vtCodec{}).Marshal(message); err != nil {
			panic(err)
		}
		if err := (vtCodec{}).Unmarshal(nil, message); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("empty request/reply allocated %g times", allocations)
	}
}

func TestUnknownWireOwnershipAndReset(t *testing.T) {
	receiver := testpb.PacketFromVTPool()
	defer receiver.ReturnToVTPool()
	raw := protowire.AppendTag(nil, 100, protowire.BytesType)
	raw = protowire.AppendBytes(raw, []byte("unknown account data"))
	if err := (vtCodec{}).Unmarshal(mem.BufferSlice{mem.SliceBuffer(raw)}, receiver); err != nil {
		t.Fatal(err)
	}
	retained := bytes.Clone(receiver.ProtoReflect().GetUnknown())
	clear(raw)
	if len(retained) == 0 || !bytes.Equal(receiver.ProtoReflect().GetUnknown(), retained) {
		t.Fatal("unknown protobuf fields borrowed wire buffer")
	}
	if err := (vtCodec{}).Unmarshal(nil, receiver); err != nil {
		t.Fatal(err)
	}
	if len(receiver.ProtoReflect().GetUnknown()) != 0 {
		t.Fatal("unknown account data survived reset")
	}
}

func TestUnmarshalRetainsCallerBufferReference(t *testing.T) {
	want := samplePacket()
	want.Payload = bytes.Repeat([]byte("payload"), 1024)
	owned, err := (vtCodec{}).Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	defer owned.Free()
	receiver := testpb.PacketFromVTPool()
	defer receiver.ReturnToVTPool()
	if err = (vtCodec{}).Unmarshal(owned, receiver); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(want, receiver) {
		t.Fatal("large pooled receive mismatch")
	}
	// MaterializeToBuffer acquires its own reference. Its deferred Free must not
	// consume the caller's still-live reference or return that buffer prematurely.
	raw := owned.Materialize()
	var check testpb.Packet
	if err = proto.Unmarshal(raw, &check); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(want, &check) {
		t.Fatal("caller input buffer was released during unmarshal")
	}
}

type trackedBufferPool struct {
	requested int
	puts      int
}

func (p *trackedBufferPool) Get(size int) *[]byte {
	p.requested = size
	data := make([]byte, size)
	return &data
}
func (p *trackedBufferPool) Put(*[]byte) { p.puts++ }

func TestSmallMarshalReturnsPooledBytesAfterLastReference(t *testing.T) {
	if mem.IsBelowBufferPoolingThreshold(minPooledBufferBytes) {
		t.Fatal("selected minimum no longer enables reference-counted pooling")
	}
	pool := &trackedBufferPool{}
	message := samplePacket()
	data, err := marshalWithPool(message, message.SizeVT(), pool)
	if err != nil {
		t.Fatal(err)
	}
	if pool.requested != minPooledBufferBytes {
		t.Fatalf("requested %d bytes", pool.requested)
	}
	if data.Len() != message.SizeVT() {
		t.Fatal("pool bucket leaked into wire message length")
	}
	extra := data[0]
	extra.Ref()
	data.Free()
	if pool.puts != 0 {
		t.Fatal("returned bytes while transport still holds a reference")
	}
	extra.Free()
	if pool.puts != 1 {
		t.Fatalf("last reference returned bytes %d times", pool.puts)
	}
}

func TestSmallFragmentMaterializationReturnsPooledBytes(t *testing.T) {
	pool := &trackedBufferPool{}
	fragments := mem.BufferSlice{mem.SliceBuffer([]byte("first")), mem.SliceBuffer([]byte("second"))}
	buffer := materializeWithPool(fragments, pool)
	if string(buffer.ReadOnlyData()) != "firstsecond" {
		t.Fatal("fragment assembly changed bytes")
	}
	buffer.Free()
	if pool.requested != minPooledBufferBytes || pool.puts != 1 {
		t.Fatal("fragment assembly did not return its pooled backing array")
	}
}

func TestFailedSmallMarshalReturnsPooledBytes(t *testing.T) {
	pool := &trackedBufferPool{}
	data, err := marshalWithPool(&badMessage{size: 1, count: 2}, 1, pool)
	if !errors.Is(err, ErrInvalidMessage) || data != nil {
		t.Fatal("accepted malformed optimized output")
	}
	if pool.puts != 1 {
		t.Fatalf("failed encoding returned backing buffer %d times", pool.puts)
	}
}

func TestPanickingSmallMarshalReturnsPooledBytes(t *testing.T) {
	pool := &trackedBufferPool{}
	data, err := marshalWithPool(&badMessage{size: 1, marshalPanic: true}, 1, pool)
	if !errors.Is(err, ErrInvalidMessage) || data != nil {
		t.Fatal("generated marshal panic escaped safe serialization boundary")
	}
	if pool.puts != 1 {
		t.Fatal("panicking encoder did not return its backing buffer")
	}
}
