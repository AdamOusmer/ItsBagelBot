// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package compare

import (
	"context"
	"crypto/tls"
	"errors"
	"github.com/coder/websocket"
	"io"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/encoding"
	_ "google.golang.org/grpc/encoding/proto"
	"google.golang.org/grpc/mem"
)

// Named codec is confined to explicit benchmark RPCs; it never replaces proto.
// Event receivers start empty and remain VT-owned, allowing ResetVT to return
// nested VT messages to their pools. Generated string decoding is safe/copying.
type vtCodec struct{}

func (vtCodec) Name() string { return "vtproto" }
func (vtCodec) Marshal(value any) (mem.BufferSlice, error) {
	event, ok := value.(*Event)
	if !ok {
		return encoding.GetCodecV2("proto").Marshal(value)
	}
	pool := mem.DefaultBufferPool()
	buffer := pool.Get(event.SizeVT())
	n, err := event.MarshalToSizedBufferVT(*buffer)
	if err != nil {
		pool.Put(buffer)
		return nil, err
	}
	*buffer = (*buffer)[:n]
	return mem.BufferSlice{mem.NewBuffer(buffer, pool)}, nil
}
func (vtCodec) Unmarshal(data mem.BufferSlice, value any) error {
	event, ok := value.(*Event)
	if !ok {
		return encoding.GetCodecV2("proto").Unmarshal(data, value)
	}
	buffer := data.MaterializeToBuffer(mem.DefaultBufferPool())
	defer buffer.Free()
	event.ResetVT()
	return event.UnmarshalVT(buffer.ReadOnlyData())
}
func init() { encoding.RegisterCodecV2(vtCodec{}) }

// This native server uses grpc.Server.Serve directly over TLS. The earlier
// net/http ServeHTTP endpoint remains available as a separately named baseline.
func startNativeGRPC(certificate tls.Certificate) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13})))
	server.RegisterService(&networkService, networkServer{})
	go server.Serve(listener)
	return "https://" + listener.Addr().String(), server.Stop, nil
}
func isGRPCMode(mode string) bool { return strings.HasPrefix(mode, "grpc-") }
func newModeGRPC(f *networkFixture, m *wireMeter, mode string) (*grpc.ClientConn, error) {
	endpoint := f.endpoint
	if strings.HasPrefix(mode, "grpc-native-") {
		endpoint = f.rpcEndpoint
	}
	options := []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(f.tls.Clone())), grpc.WithContextDialer(m.dial), grpc.WithDisableRetry(), grpc.WithDisableServiceConfig()}
	if mode == "grpc-native-vtprotobuf" {
		options = append(options, grpc.WithDefaultCallOptions(grpc.ForceCodecV2(vtCodec{})))
	}
	return grpc.NewClient(strings.TrimPrefix(endpoint, "https://"), options...)
}
func unmarshalNetworkProto(mode string, data []byte, event *Event) error {
	if mode == "ws-vtprotobuf" {
		event.ResetVT()
		return event.UnmarshalVT(data)
	}
	return standardNetworkUnmarshal(data, event)
}

func TestNativeGRPCStreamIsolation(t *testing.T) {
	fixture := startNetworkServer(t)
	for _, mode := range []string{"grpc-native-protobuf", "grpc-native-vtprotobuf"} {
		t.Run(mode, func(t *testing.T) {
			meter := &wireMeter{}
			channel, err := newModeGRPC(fixture, meter, mode)
			if err != nil {
				t.Fatal(err)
			}
			defer channel.Close()
			ctx, cancel := context.WithCancel(t.Context())
			primary, err := grpcStream(ctx, channel, "chat", -1, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = primary.Header(); err != nil {
				t.Fatal(err)
			}
			pending, err := grpcStream(t.Context(), channel, "chat", 2, 0)
			if err != nil {
				t.Fatal(err)
			}
			var event Event
			want := SampleEvent("chat")
			receiveNativeEvent(t, pending, &event, want)
			cancel()
			if err = primary.RecvMsg(&Event{}); err == nil {
				t.Fatal("cancelled old stream succeeded")
			}
			want.Sequence++
			receiveNativeEvent(t, pending, &event, want)
			if err = pending.RecvMsg(&event); err != io.EOF {
				t.Fatalf("native EOF: %v", err)
			}
			if meter.connections.Load() != 1 {
				t.Fatalf("native streams used %d physical connections", meter.connections.Load())
			}
		})
	}
}

func TestWSVTNetworkPayloads(t *testing.T) {
	fixture := startNetworkServer(t)
	for _, sample := range []string{"chat", "chat_large", "alert"} {
		t.Run(sample, func(t *testing.T) {
			socket, err := wsStream(t.Context(), fixture, &wireMeter{}, "ws-vtprotobuf", sample, 2, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer socket.CloseNow()
			if err = socket.Write(t.Context(), websocket.MessageText, []byte("start")); err != nil {
				t.Fatal(err)
			}
			var event Event
			var jsonEvent JSONEvent
			var buffer [16 << 10]byte
			want := SampleEvent(sample)
			for i := 0; i < 2; i++ {
				body, err := readWS(t.Context(), socket, buffer[:])
				if err != nil {
					t.Fatal(err)
				}
				if !decodeNetwork("ws-vtprotobuf", body, &jsonEvent, &event, want) {
					t.Fatal("VT payload mismatch")
				}
				want.Sequence++
			}
			if err = finishReceive(t.Context(), nil, socket, &event, buffer[:]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func receiveNativeEvent(t *testing.T, stream grpc.ClientStream, event, want *Event) {
	t.Helper()
	if err := stream.RecvMsg(event); err != nil {
		t.Fatal(err)
	}
	if !validateNetworkEvent(event, want) {
		t.Fatal("native event payload mismatch")
	}
}

func nativeLifecycleReceive(ctx context.Context, fixture *networkFixture, meter *wireMeter, mode string, shared *grpc.ClientConn) error {
	channel := shared
	if channel == nil {
		var err error
		channel, err = newModeGRPC(fixture, meter, mode)
		if err != nil {
			return err
		}
		defer channel.Close()
	}
	stream, err := grpcStream(ctx, channel, "chat", 1, 0)
	if err != nil {
		return err
	}
	var event Event
	if mode == "grpc-native-vtprotobuf" {
		defer event.ResetVT()
	}
	if err = stream.RecvMsg(&event); err != nil {
		return err
	}
	if !validateNetworkEvent(&event, SampleEvent("chat")) {
		return errors.New("native lifecycle payload mismatch")
	}
	if err = stream.RecvMsg(&event); err != io.EOF {
		return errors.New("native lifecycle did not end after one event")
	}
	return nil
}
func BenchmarkNativeLifecycle(b *testing.B) {
	fixture := startNetworkServer(b)
	for _, example := range []struct {
		name, mode string
		warm       bool
	}{
		{"cold-standard", "grpc-native-protobuf", false},
		{"warm-standard", "grpc-native-protobuf", true},
		{"warm-vt", "grpc-native-vtprotobuf", true},
	} {
		b.Run(example.name, func(b *testing.B) { benchmarkNativeLifecycle(b, fixture, example.mode, example.warm) })
	}
}
func benchmarkNativeLifecycle(b *testing.B, fixture *networkFixture, mode string, warm bool) {
	ctx := context.Background()
	meter := &wireMeter{}
	var shared *grpc.ClientConn
	if warm {
		var err error
		shared, err = newModeGRPC(fixture, meter, mode)
		if err != nil {
			b.Fatal(err)
		}
		defer shared.Close()
		if err = nativeLifecycleReceive(ctx, fixture, meter, mode, shared); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	cpuStart := clientCPU()
	for i := 0; i < b.N; i++ {
		if err := nativeLifecycleReceive(ctx, fixture, meter, mode, shared); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if elapsed := clientCPU() - cpuStart; elapsed > 0 {
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "cpu-ns/session")
	}
	b.ReportMetric(float64(meter.connections.Load()), "TLS-connections")
}
