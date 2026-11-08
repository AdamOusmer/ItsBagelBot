// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package compare

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/grpc"
)

func residentKiB() int64 {
	raw, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return -1
	}
	value, _ := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	return value
}

func TestIdleFootprint(t *testing.T) {
	if os.Getenv("BAGEL_NETWORK_IDLE") != "1" {
		t.Skip("explicit fresh-process idle measurements")
	}
	fixture := startNetworkServer(t)
	// Obtain the public test certificate before launching independent client processes.
	connection, err := tls.Dial("tcp", strings.TrimPrefix(fixture.endpoint, "https://"), fixture.tls.Clone())
	if err != nil {
		t.Fatal(err)
	}
	der := connection.ConnectionState().PeerCertificates[0].Raw
	connection.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range idleModes() {
		for _, count := range []int{1, 20} {
			command := exec.Command(executable, "-test.run=^TestIdleClientProcess$", "-test.v")
			command.Env = append(os.Environ(), "BAGEL_IDLE_CHILD=1", "BAGEL_IDLE_MODE="+mode, "BAGEL_IDLE_COUNT="+strconv.Itoa(count), "BAGEL_IDLE_ENDPOINT="+fixture.endpoint, "BAGEL_IDLE_RPC_ENDPOINT="+fixture.rpcEndpoint, "BAGEL_IDLE_CERT="+base64.StdEncoding.EncodeToString(der))
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("%s/%d: %v %s", mode, count, err, output)
			}
			for _, line := range strings.Split(string(output), "\n") {
				if strings.HasPrefix(line, "IDLE ") {
					t.Log(line)
				}
			}
		}
	}
}

func TestIdleClientProcess(t *testing.T) {
	if os.Getenv("BAGEL_IDLE_CHILD") != "1" {
		t.Skip("isolated client helper")
	}
	mode := os.Getenv("BAGEL_IDLE_MODE")
	count, _ := strconv.Atoi(os.Getenv("BAGEL_IDLE_COUNT"))
	der, err := base64.StdEncoding.DecodeString(os.Getenv("BAGEL_IDLE_CERT"))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	fixture := &networkFixture{endpoint: os.Getenv("BAGEL_IDLE_ENDPOINT"), rpcEndpoint: os.Getenv("BAGEL_IDLE_RPC_ENDPOINT"), tls: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	rssBefore := residentKiB()
	goroutinesBefore := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	meter := &wireMeter{}
	// Each retained connection represents a separate app: gRPC uses one channel
	// per app here, rather than giving it an unfair 20-stream/one-channel baseline.
	channels := make([]*grpc.ClientConn, 0, count)
	sockets := make([]*websocket.Conn, 0, count)
	for i := 0; i < count; i++ {
		channel, socket, err := openIdle(ctx, fixture, meter, mode)
		if err != nil {
			t.Fatal(err)
		}
		if channel != nil {
			channels = append(channels, channel)
		}
		if socket != nil {
			sockets = append(sockets, socket)
		}
	}

	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	rssAfter := residentKiB()
	goroutinesAfter := runtime.NumGoroutine()
	cpuBefore := clientCPU()
	wireBefore := meter.read.Load() + meter.write.Load()
	time.Sleep(time.Second)
	cpuDelta := clientCPU() - cpuBefore
	wireDelta := meter.read.Load() + meter.write.Load() - wireBefore
	fmt.Printf("IDLE %s apps=%d heap_delta_B=%d rss_delta_KiB=%d goroutines_delta=%d idle_cpu_us_per_s=%d idle_TLS_B_per_s=%d TLS_connections=%d\n", mode, count, int64(after.HeapAlloc)-int64(before.HeapAlloc), rssAfter-rssBefore, goroutinesAfter-goroutinesBefore, cpuDelta.Microseconds(), wireDelta, meter.connections.Load())
	for _, socket := range sockets {
		socket.CloseNow()
	}
	for _, channel := range channels {
		channel.Close()
	}
}

func openIdle(ctx context.Context, fixture *networkFixture, meter *wireMeter, mode string) (*grpc.ClientConn, *websocket.Conn, error) {
	if isGRPCMode(mode) {
		channel, err := newModeGRPC(fixture, meter, mode)
		if err != nil {
			return nil, nil, err
		}
		stream, err := grpcStream(ctx, channel, "chat", -1, 0)
		if err == nil {
			_, err = stream.Header()
		}
		return channel, nil, err
	}
	socket, err := wsStream(ctx, fixture, meter, mode, "chat", -1, 0)
	if err == nil {
		err = socket.Write(ctx, websocket.MessageText, []byte("start"))
	}
	return nil, socket, err
}

func idleModes() []string {
	if os.Getenv("BAGEL_NETWORK_OPTIMIZED") == "1" {
		return []string{"ws-protobuf", "ws-vtprotobuf", "grpc-native-protobuf", "grpc-native-vtprotobuf"}
	}
	return []string{"ws-json", "ws-protobuf", "grpc-protobuf"}
}
