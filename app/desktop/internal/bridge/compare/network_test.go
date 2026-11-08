// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package compare

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/pkg/codec"
	"github.com/coder/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const networkMethod = "/bagel.benchmark.Compare/Listen"

type benchServer interface{ Listen(grpc.ServerStream) error }
type networkServer struct{}

var networkService = grpc.ServiceDesc{ServiceName: "bagel.benchmark.Compare", HandlerType: (*benchServer)(nil), Streams: []grpc.StreamDesc{{StreamName: "Listen", ServerStreams: true, Handler: func(s any, stream grpc.ServerStream) error { return s.(benchServer).Listen(stream) }}}}

func (networkServer) Listen(stream grpc.ServerStream) error {
	var empty emptypb.Empty
	if err := stream.RecvMsg(&empty); err != nil {
		return err
	}
	md, _ := metadata.FromIncomingContext(stream.Context())
	count, _ := strconv.Atoi(firstMetadata(md, "x-count"))
	pace, _ := strconv.Atoi(firstMetadata(md, "x-pace"))
	if firstMetadata(md, "authorization") != "Bearer benchmark" {
		return errors.New("missing auth")
	}
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return err
	}
	event := SampleEvent(firstMetadata(md, "x-sample"))
	return sendNetworkEvents(stream.Context(), event, count, pace, func(e *Event) error { return stream.SendMsg(e) })
}
func firstMetadata(md metadata.MD, key string) string {
	v := md.Get(key)
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
func sendNetworkEvents(ctx context.Context, e *Event, count, pace int, send func(*Event) error) error {
	if count < 0 {
		<-ctx.Done()
		return ctx.Err()
	}
	seed := e.Sequence
	var ticker *time.Ticker
	if pace > 0 {
		ticker = time.NewTicker(time.Second / time.Duration(pace))
		defer ticker.Stop()
	}
	for i := 0; i < count; i++ {
		e.Sequence = seed + uint64(i)
		if ticker != nil {
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return ctx.Err()
			}
			e.SentUnixNano = time.Now().UnixNano()
		}
		if err := send(e); err != nil {
			return err
		}
	}
	return nil
}
func networkWS(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer benchmark" {
		w.WriteHeader(401)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	if _, _, err = conn.Read(r.Context()); err != nil {
		return
	}
	q := r.URL.Query()
	count, _ := strconv.Atoi(q.Get("count"))
	pace, _ := strconv.Atoi(q.Get("pace"))
	if count < 0 {
		_, _, _ = conn.Read(r.Context())
		return
	}
	sample := SampleEvent(q.Get("sample"))
	mode := q.Get("mode")
	err = sendNetworkEvents(r.Context(), sample, count, pace, func(e *Event) error {
		var data []byte
		if mode == "ws-json" {
			data, _ = codec.Marshal(AsJSON(e))
		} else if mode == "ws-vtprotobuf" {
			data, _ = e.MarshalVT()
		} else {
			data, _ = proto.Marshal(e)
		}
		kind := websocket.MessageBinary
		if mode == "ws-json" {
			kind = websocket.MessageText
		}
		return conn.Write(r.Context(), kind, data)
	})
	if err == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}
}
func TestNetworkServerProcess(t *testing.T) {
	if os.Getenv("BAGEL_NETWORK_CHILD") != "1" {
		t.Skip("isolated benchmark helper")
	}
	rpc := grpc.NewServer()
	rpc.RegisterService(&networkService, networkServer{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			rpc.ServeHTTP(w, r)
			return
		}
		networkWS(w, r)
	})
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = true
	server.StartTLS()
	nativeURL, stopNative, err := startNativeGRPC(server.TLS.Certificates[0])
	if err != nil {
		t.Fatal(err)
	}
	defer stopNative()
	fmt.Println("BENCH_READY " + server.URL + " " + base64.StdEncoding.EncodeToString(server.TLS.Certificates[0].Certificate[0]) + " " + nativeURL)
	_, _ = io.Copy(io.Discard, os.Stdin)
	server.Close()
	rpc.Stop()
}

type networkFixture struct {
	endpoint    string
	rpcEndpoint string
	tls         *tls.Config
}

func startNetworkServer(t testing.TB) *networkFixture {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestNetworkServerProcess$")
	command.Env = append(os.Environ(), "BAGEL_NETWORK_CHILD=1")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close(); _ = command.Wait() })
	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) != 4 || parts[0] != "BENCH_READY" {
			continue
		}
		certDER, err := base64.StdEncoding.DecodeString(parts[2])
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(certDER)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		roots.AddCert(cert)
		return &networkFixture{endpoint: parts[1], rpcEndpoint: parts[3], tls: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}}
	}
	t.Fatal("benchmark child did not become ready")
	return nil
}

type wireMeter struct{ read, write, connections atomic.Int64 }
type meteredConn struct {
	net.Conn
	meter *wireMeter
}

func (c *meteredConn) Read(p []byte) (int, error) {
	n, e := c.Conn.Read(p)
	c.meter.read.Add(int64(n))
	return n, e
}
func (c *meteredConn) Write(p []byte) (int, error) {
	n, e := c.Conn.Write(p)
	c.meter.write.Add(int64(n))
	return n, e
}
func (m *wireMeter) dial(ctx context.Context, address string) (net.Conn, error) {
	c, e := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if e != nil {
		return nil, e
	}
	m.connections.Add(1)
	return &meteredConn{Conn: c, meter: m}, nil
}
func newGRPC(f *networkFixture, m *wireMeter) (*grpc.ClientConn, error) {
	return grpc.NewClient(strings.TrimPrefix(f.endpoint, "https://"), grpc.WithTransportCredentials(credentials.NewTLS(f.tls.Clone())), grpc.WithContextDialer(m.dial), grpc.WithDisableRetry(), grpc.WithDisableServiceConfig())
}
func grpcStream(ctx context.Context, c *grpc.ClientConn, sample string, count, pace int) (grpc.ClientStream, error) {
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer benchmark", "x-count", strconv.Itoa(count), "x-pace", strconv.Itoa(pace), "x-sample", sample))
	stream, err := c.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, networkMethod)
	if err == nil {
		err = stream.SendMsg(&emptypb.Empty{})
	}
	if err == nil {
		err = stream.CloseSend()
	}
	return stream, err
}
func wsStream(ctx context.Context, f *networkFixture, m *wireMeter, mode, sample string, count, pace int) (*websocket.Conn, error) {
	transport := &http.Transport{TLSClientConfig: f.tls.Clone(), DialContext: func(ctx context.Context, _, address string) (net.Conn, error) { return m.dial(ctx, address) }}
	endpoint := "wss" + strings.TrimPrefix(f.endpoint, "https") + "/ws?mode=" + mode + "&sample=" + sample + "&count=" + strconv.Itoa(count) + "&pace=" + strconv.Itoa(pace)
	conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}, HTTPHeader: http.Header{"Authorization": []string{"Bearer benchmark"}}, CompressionMode: websocket.CompressionDisabled})
	return conn, err
}
func readWS(ctx context.Context, c *websocket.Conn, buffer []byte) ([]byte, error) {
	_, reader, err := c.Reader(ctx)
	if err != nil {
		return nil, err
	}
	n, err := io.ReadFull(reader, buffer)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return buffer[:n], nil
	}
	if err == nil {
		return nil, errors.New("frame exceeds benchmark buffer")
	}
	return nil, err
}
func validateNetworkEvent(event *Event, want *Event) bool {
	if event.Sequence != want.Sequence || event.SentUnixNano != want.SentUnixNano || event.Kind != want.Kind {
		return false
	}
	if want.Chat != nil {
		return sameNetworkChat(event, want)
	}
	return sameNetworkAlert(event, want)
}
func sameNetworkChat(event, want *Event) bool {
	return event.Chat != nil && event.Alert == nil && event.Chat.ChannelId == want.Chat.ChannelId && event.Chat.UserId == want.Chat.UserId && event.Chat.DisplayName == want.Chat.DisplayName && event.Chat.Text == want.Chat.Text
}
func sameNetworkAlert(event, want *Event) bool {
	return event.Alert != nil && event.Chat == nil && event.Alert.Kind == want.Alert.Kind && event.Alert.Name == want.Alert.Name && event.Alert.Amount == want.Alert.Amount
}
func decodeNetwork(mode string, data []byte, jsonEvent *JSONEvent, event *Event, want *Event) bool {
	if mode == "ws-json" {
		if codec.FastUnmarshal(data, jsonEvent) != nil {
			return false
		}
		return sameSemantic(*jsonEvent, want)
	}
	return unmarshalNetworkProto(mode, data, event) == nil && validateNetworkEvent(event, want)
}

func prepareReceive(b *testing.B, ctx context.Context, fixture *networkFixture, meter *wireMeter, mode, sample string, event *Event) (*grpc.ClientConn, *websocket.Conn) {
	var rpc *grpc.ClientConn
	var socket *websocket.Conn
	var err error
	if isGRPCMode(mode) {
		rpc, err = newModeGRPC(fixture, meter, mode)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { _ = rpc.Close() })
		// Warm the TLS channel using a completed one-event stream.
		warm, e := grpcStream(ctx, rpc, sample, 1, 0)
		if e != nil {
			b.Fatal(e)
		}
		if e = warm.RecvMsg(event); e != nil {
			b.Fatal(e)
		}
		if e = warm.RecvMsg(event); e != io.EOF {
			b.Fatalf("warm EOF: %v", e)
		}
	} else {
		socket, err = wsStream(ctx, fixture, meter, mode, sample, b.N+1, 0)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { _ = socket.CloseNow() })
	}
	return rpc, socket
}
func startReceive(b *testing.B, ctx context.Context, rpc *grpc.ClientConn, socket *websocket.Conn, sample string) (grpc.ClientStream, error) {
	if rpc != nil {
		return grpcStream(ctx, rpc, sample, b.N+1, 0)
	}
	return nil, socket.Write(ctx, websocket.MessageText, []byte("start"))
}
func warmReceive(t testing.TB, ctx context.Context, stream grpc.ClientStream, socket *websocket.Conn, mode string, buffer []byte, event *Event, jsonEvent *JSONEvent, want *Event) {
	t.Helper()
	var err error
	if stream != nil {
		err = stream.RecvMsg(event)
	} else {
		var data []byte
		data, err = readWS(ctx, socket, buffer)
		if err == nil && !decodeNetwork(mode, data, jsonEvent, event, want) {
			t.Fatal("warm payload mismatch")
		}
	}
	if err != nil {
		t.Fatal(err)
	}
}
func finishReceive(ctx context.Context, stream grpc.ClientStream, socket *websocket.Conn, event *Event, buffer []byte) error {
	if stream != nil {
		if err := stream.RecvMsg(event); err != io.EOF {
			return fmt.Errorf("terminal EOF: %v", err)
		}
		return nil
	}
	_, err := readWS(ctx, socket, buffer)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		return fmt.Errorf("terminal close: %v", err)
	}
	return nil
}
func BenchmarkNetworkReceive(b *testing.B) {
	fixture := startNetworkServer(b)
	for _, sample := range []string{"chat", "chat_large", "alert"} {
		for _, mode := range networkModes() {
			b.Run(sample+"/"+mode, func(b *testing.B) { benchmarkReceiveCase(b, fixture, mode, sample) })
		}
	}
}

func benchmarkReceiveCase(b *testing.B, fixture *networkFixture, mode, sample string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	meter := &wireMeter{}
	want := SampleEvent(sample)
	var event Event
	var jsonEvent JSONEvent
	var buffer [16 << 10]byte
	rpc, socket := prepareReceive(b, ctx, fixture, meter, mode, sample, &event)
	beforeRead, beforeWrite := meter.read.Load(), meter.write.Load()
	stream, err := startReceive(b, ctx, rpc, socket, sample)
	if err != nil {
		b.Fatal(err)
	}
	warmReceive(b, ctx, stream, socket, mode, buffer[:], &event, &jsonEvent, want)
	b.ReportAllocs()
	b.ResetTimer()
	cpuStart := clientCPU()
	for i := 0; i < b.N; i++ {
		want.Sequence++
		if stream != nil {
			if err = stream.RecvMsg(&event); err != nil {
				b.Fatal(err)
			}
			if !validateNetworkEvent(&event, want) {
				b.Fatal("gRPC payload mismatch")
			}
		} else {
			data, e := readWS(ctx, socket, buffer[:])
			if e != nil {
				b.Fatal(e)
			}
			if !decodeNetwork(mode, data, &jsonEvent, &event, want) {
				b.Fatal("WS payload mismatch")
			}
		}
	}
	if err := finishReceive(ctx, stream, socket, &event, buffer[:]); err != nil {
		b.Fatal(err)
	}
	b.StopTimer()
	if elapsed := clientCPU() - cpuStart; elapsed > 0 {
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "cpu-ns/event")
	}
	b.ReportMetric(float64(meter.read.Load()-beforeRead)/float64(b.N), "TCP-read-B/event")
	b.ReportMetric(float64(meter.write.Load()-beforeWrite)/float64(b.N), "TCP-write-B/event")
	b.ReportMetric(float64(meter.connections.Load()), "TLS-connections")
}

func preparePaced(t *testing.T, ctx context.Context, fixture *networkFixture, meter *wireMeter, mode, sample string, count, pace int) (grpc.ClientStream, *websocket.Conn) {
	var rpc *grpc.ClientConn
	var stream grpc.ClientStream
	var socket *websocket.Conn
	var err error
	if isGRPCMode(mode) {
		rpc, err = newModeGRPC(fixture, meter, mode)
		if err == nil {
			t.Cleanup(func() { _ = rpc.Close() })
			stream, err = grpcStream(ctx, rpc, sample, count, pace)
		}
	} else {
		socket, err = wsStream(ctx, fixture, meter, mode, sample, count, pace)
		if err == nil {
			t.Cleanup(func() { _ = socket.CloseNow() })
			err = socket.Write(ctx, websocket.MessageText, []byte("start"))
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	return stream, socket
}
func readPaced(ctx context.Context, stream grpc.ClientStream, socket *websocket.Conn, mode string, buffer []byte, event *Event, jsonEvent *JSONEvent) (int64, error) {
	var err error
	sent := int64(0)
	if stream != nil {
		err = stream.RecvMsg(event)
		sent = event.SentUnixNano
	} else {
		var data []byte
		data, err = readWS(ctx, socket, buffer)
		if err == nil {
			if mode == "ws-json" {
				err = codec.FastUnmarshal(data, jsonEvent)
				sent = jsonEvent.SentUnixNano
			} else {
				err = unmarshalNetworkProto(mode, data, event)
				sent = event.SentUnixNano
			}
		}
	}
	return sent, err
}
func TestPacedNetworkLatency(t *testing.T) {
	if os.Getenv("BAGEL_NETWORK_LATENCY") != "1" {
		t.Skip("explicit one-second paced network measurements")
	}
	fixture := startNetworkServer(t)
	for _, mode := range networkModes() {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			meter := &wireMeter{}
			sample := "chat"
			count := 1000
			pace := 1000
			if os.Getenv("BAGEL_NETWORK_PACE") == "100" {
				pace = 100
				count = 300
			}
			stream, socket := preparePaced(t, ctx, fixture, meter, mode, sample, count, pace)
			want := SampleEvent(sample)
			seed := want.Sequence
			latencies := make([]int64, 0, count)
			var event Event
			var jsonEvent JSONEvent
			var buffer [16 << 10]byte
			cpuStart := clientCPU()
			for i := 0; i < count; i++ {
				sent, err := readPaced(ctx, stream, socket, mode, buffer[:], &event, &jsonEvent)
				if err != nil {
					t.Fatal(err)
				}
				want.Sequence = seed + uint64(i)
				want.SentUnixNano = sent
				valid := validateNetworkEvent(&event, want)
				if mode == "ws-json" {
					valid = sameSemantic(jsonEvent, want)
				}
				if !valid {
					t.Fatal("paced payload/order mismatch")
				}
				latency := time.Now().UnixNano() - sent
				if latency < 0 {
					t.Fatal("negative local clock latency")
				}
				latencies = append(latencies, latency)
			}
			if err := finishReceive(ctx, stream, socket, &event, buffer[:]); err != nil {
				t.Fatal(err)
			}
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			t.Logf("rate=%d samples=%d client_cpu_us/event=%.3f local send-to-decoded: p50=%s p95=%s p99=%s", pace, count, float64((clientCPU()-cpuStart).Nanoseconds())/float64(count)/1000, time.Duration(latencies[(count-1)*50/100]), time.Duration(latencies[(count-1)*95/100]), time.Duration(latencies[(count-1)*99/100]))
		})
	}
}

func lifecycleReceive(ctx context.Context, fixture *networkFixture, meter *wireMeter, mode string, shared *grpc.ClientConn) error {
	if mode == "ws-json" || mode == "ws-protobuf" {
		return lifecycleWS(ctx, fixture, meter, mode)
	}
	return lifecycleGRPC(ctx, fixture, meter, shared)
}
func lifecycleWS(ctx context.Context, fixture *networkFixture, meter *wireMeter, mode string) error {
	want := SampleEvent("chat")
	var event Event
	var jsonEvent JSONEvent
	var buffer [16 << 10]byte
	conn, err := wsStream(ctx, fixture, meter, mode, "chat", 1, 0)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	if err = conn.Write(ctx, websocket.MessageText, []byte("start")); err != nil {
		return err
	}
	data, err := readWS(ctx, conn, buffer[:])
	if err != nil {
		return err
	}
	if !decodeNetwork(mode, data, &jsonEvent, &event, want) {
		return errors.New("lifecycle WS payload mismatch")
	}
	_, err = readWS(ctx, conn, buffer[:])
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		return err
	}
	return nil
}
func lifecycleGRPC(ctx context.Context, fixture *networkFixture, meter *wireMeter, shared *grpc.ClientConn) error {
	want := SampleEvent("chat")
	var event Event

	channel := shared
	if channel == nil {
		var err error
		channel, err = newGRPC(fixture, meter)
		if err != nil {
			return err
		}
		defer channel.Close()
	}
	stream, err := grpcStream(ctx, channel, "chat", 1, 0)
	if err != nil {
		return err
	}
	if err = stream.RecvMsg(&event); err != nil {
		return err
	}
	if !validateNetworkEvent(&event, want) {
		return errors.New("lifecycle gRPC payload mismatch")
	}
	if err = stream.RecvMsg(&event); err != io.EOF {
		return errors.New("lifecycle stream did not terminate exactly once")
	}
	return nil
}

// Includes connection/RPC creation, first fully consumed event, terminal control
// framing, and cleanup. Cold modes include a new TLS handshake every operation.
func BenchmarkNetworkLifecycle(b *testing.B) {
	fixture := startNetworkServer(b)
	for _, mode := range []string{"ws-json", "ws-protobuf", "grpc-cold", "grpc-warm-stream"} {
		b.Run(mode, func(b *testing.B) {
			meter := &wireMeter{}
			ctx := context.Background()
			var shared *grpc.ClientConn
			if mode == "grpc-warm-stream" {
				var err error
				shared, err = newGRPC(fixture, meter)
				if err != nil {
					b.Fatal(err)
				}
				defer shared.Close()
				if err = lifecycleReceive(ctx, fixture, meter, mode, shared); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			cpuStart := clientCPU()
			for i := 0; i < b.N; i++ {
				if err := lifecycleReceive(ctx, fixture, meter, mode, shared); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if elapsed := clientCPU() - cpuStart; elapsed > 0 {
				b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "cpu-ns/session")
			}
			b.ReportMetric(float64(meter.connections.Load()), "TLS-connections")
		})
	}
}

func TestNetworkStreamOverlapAndEOF(t *testing.T) {
	fixture := startNetworkServer(t)
	meter := &wireMeter{}
	channel, err := newGRPC(fixture, meter)
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	primaryCtx, cancel := context.WithCancel(t.Context())
	primary, err := grpcStream(primaryCtx, channel, "chat", -1, 0)
	if err != nil {
		t.Fatal(err)
	}
	pendingCtx, stop := context.WithCancel(t.Context())
	defer stop()
	pending, err := grpcStream(pendingCtx, channel, "chat", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	var event Event
	if err = pending.RecvMsg(&event); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err = primary.RecvMsg(&event); err == nil {
		t.Fatal("cancelled primary returned event")
	}
	if err = pending.RecvMsg(&event); err != nil {
		t.Fatal("primary cancellation affected pending:", err)
	}
	if err = pending.RecvMsg(&event); err != io.EOF {
		t.Fatalf("pending EOF: %v", err)
	}
	if meter.connections.Load() != 1 {
		t.Fatalf("overlapping streams created %d TLS connections", meter.connections.Load())
	}
}

func networkModes() []string {
	if os.Getenv("BAGEL_NETWORK_ORDER") == "reverse" {
		return []string{"grpc-native-vtprotobuf", "ws-vtprotobuf", "grpc-native-protobuf", "grpc-protobuf", "ws-protobuf", "ws-json"}
	}
	return []string{"ws-json", "ws-protobuf", "grpc-protobuf", "grpc-native-protobuf", "ws-vtprotobuf", "grpc-native-vtprotobuf"}
}

func standardNetworkUnmarshal(data []byte, event *Event) error { return proto.Unmarshal(data, event) }
