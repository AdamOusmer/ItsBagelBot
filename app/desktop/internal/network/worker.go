// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"os"
	"sync"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/network/wire"
	"ItsBagelBot/app/desktop/internal/oauth"
	"ItsBagelBot/pkg/protobuf"
	"ItsBagelBot/pkg/sandbox"
	shared "ItsBagelBot/pkg/websocket"
)

type worker struct {
	peer        *peer
	mu          sync.Mutex
	streams     [streamCount]*workerStream
	native      *shared.Client
	oauth       *oauth.WorkerNetwork
	initialized bool
	dialer      *virtualDialer
	files       []*os.File
	operations  [4]*workerOperation
	wg          sync.WaitGroup
	closed      bool
	closeOnce   sync.Once
}

type workerStream struct {
	id     uint32
	ctx    context.Context
	socket *shared.Conn
	ack    chan struct{}
	cancel context.CancelFunc
}

// RunWorker refuses bootstrap unless the current process has kernel-enforced
// isolation. Its only inherited resources are explicit pipes and public roots.
func RunWorker() error {
	if !sandbox.ChildIsolated() {
		return ErrWorker
	}
	files, err := parseWorkerFiles(os.Args[1:])
	if err != nil {
		return err
	}
	defer closeFiles(files)
	input, output, err := sandbox.ControlPipes()
	if err != nil {
		return ErrWorker
	}
	defer input.Close()
	defer output.Close()
	p := newPeer(context.Background(), input, output)
	p.receives = workerReceives
	w := &worker{peer: p, files: files}
	p.handle = w.handle
	if err = p.send.write(&wire.Frame{Kind: hello, Isolated: true}); err != nil {
		return err
	}
	p.run()
	w.close()
	return ErrWorker
}

func (w *worker) closeIO() {
	if closer, ok := w.peer.read.reader.(io.Closer); ok {
		_ = closer.Close()
	}
	if closer, ok := w.peer.send.writer.(io.Closer); ok {
		_ = closer.Close()
	}
}

func (w *worker) handle(request *wire.Frame) bool {
	switch request.Kind {
	case bootstrap:
		w.initialize(request)
	case oauthRequest, dialRequest, postRequest:
		return w.dispatch(request)
	case closeStream:
		w.stopStream(request.Stream)
	case ackStream:
		w.acknowledge(request.Stream)
	case cancelRequest:
		w.cancelOperation(request.Id)
	default:
		w.peer.cancel()
	}
	return false
}

func (w *worker) initialize(request *wire.Frame) {
	if w.initialized || !validBootstrap(request) {
		w.peer.reply(request, &wire.Frame{Error: protocolError})
		return
	}
	settings, err := workerTLS(request.RootsPem)
	if err != nil {
		w.peer.reply(request, &wire.Frame{Error: protocolError})
		return
	}
	w.dialer = newVirtualDialer(w.peer, w.files, [2]string{request.AuthEndpoint, request.BridgeEndpoint})
	w.oauth, err = oauth.NewWorkerNetwork(oauth.WorkerNetworkConfig{Endpoint: request.AuthEndpoint, TLSConfig: settings, DialContext: w.dialer.DialContext})
	if err == nil {
		err = w.initializeBridge(request.BridgeEndpoint, settings)
	}
	if err != nil {
		w.peer.reply(request, &wire.Frame{Error: unavailable})
		return
	}
	w.initialized = true
	w.peer.reply(request, &wire.Frame{})
}

func validBootstrap(r *wire.Frame) bool {
	return validOrigin(r.AuthEndpoint) && (r.BridgeEndpoint == "" || validOrigin(r.BridgeEndpoint))
}

func (w *worker) initializeBridge(endpoint string, settings *tls.Config) error {
	if endpoint == "" {
		return nil
	}
	var err error
	w.native, err = shared.NewClient(shared.Config{Endpoint: endpoint, TLSConfig: settings, MaxMessageBytes: protocol.MaxFrameBytes, Subprotocol: protocol.Subprotocol, DialContext: w.dialer.DialContext})
	return err
}

func workerTLS(pem []byte) (*tls.Config, error) {
	if len(pem) == 0 {
		return nil, nil
	}
	if len(pem) > 64<<10 {
		return nil, ErrProtocol
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, ErrProtocol
	}
	return &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, nil
}

func (w *worker) dispatch(request *wire.Frame) bool {
	if !w.initialized {
		w.peer.reply(request, &wire.Frame{Error: protocolError})
		return false
	}
	stream, code := w.prepareStream(request)
	if code != noError {
		w.peer.reply(request, &wire.Frame{Error: code})
		return false
	}
	operation := w.reserveOperation(request, stream)
	if operation == nil {
		if stream != nil {
			w.finishStream(stream)
		}
		w.peer.reply(request, &wire.Frame{Error: unavailable})
		return false
	}
	go func() {
		defer w.wg.Done()
		defer w.releaseOperation(operation)
		defer request.ReturnToVTPool()
		w.perform(operation.ctx, request, stream)
	}()
	return true
}

func (w *worker) prepareStream(request *wire.Frame) (*workerStream, uint32) {
	if request.Kind != dialRequest {
		return nil, noError
	}
	if w.native == nil || request.Path != protocol.EventsPath || request.Stream == 0 {
		return nil, protocolError
	}
	stream := w.reserveStream(request.Stream)
	if stream == nil {
		return nil, unavailable
	}
	return stream, noError
}

type workerOperation struct {
	id     uint64
	ctx    context.Context
	cancel context.CancelFunc
	stream *workerStream
}

func (w *worker) reserveOperation(request *wire.Frame, stream *workerStream) *workerOperation {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || request.Id == 0 {
		return nil
	}
	for _, call := range w.operations {
		if call != nil && call.id == request.Id {
			return nil
		}
	}
	for i, call := range w.operations {
		if call != nil {
			continue
		}
		operation := newWorkerOperation(w.peer.ctx, request.Id, stream)
		w.operations[i] = operation
		w.wg.Add(1)
		return operation
	}
	return nil
}

func newWorkerOperation(parent context.Context, id uint64, stream *workerStream) *workerOperation {
	operation := &workerOperation{id: id, stream: stream}
	if stream != nil {
		operation.ctx = stream.ctx
		operation.cancel = stream.cancel
	} else {
		operation.ctx, operation.cancel = context.WithTimeout(parent, 5*time.Second)
	}
	return operation
}

func (w *worker) releaseOperation(operation *workerOperation) {
	if operation.stream == nil {
		operation.cancel()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, call := range w.operations {
		if call == operation {
			w.operations[i] = nil
			return
		}
	}
}

func (w *worker) cancelOperation(id uint64) {
	w.mu.Lock()
	var cancel context.CancelFunc
	for _, operation := range w.operations {
		if operation != nil && operation.id == id {
			cancel = operation.cancel
			break
		}
	}
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (w *worker) perform(ctx context.Context, request *wire.Frame, stream *workerStream) {
	switch request.Kind {
	case dialRequest:
		w.dial(request, stream)
	case oauthRequest:
		w.exchange(ctx, request)
	case postRequest:
		w.post(ctx, request)
	}
}

func (w *worker) exchange(ctx context.Context, request *wire.Frame) {
	reply, err := w.oauth.Do(ctx, oauth.NetworkRequest{Endpoint: request.Path, Proof: request.Proof, AccessToken: request.Token, Form: request.Form, Revoke: request.Revoke})
	t := reply.Token
	w.peer.reply(request, &wire.Frame{Status: int32(reply.Status), Nonce: reply.Nonce, Error: errorCode(err), Oauth: &wire.OAuthReply{Access: t.Access, TokenType: t.Type, Expires: t.Expires, Refresh: t.Refresh, Grant: t.Grant, Device: t.Device, Error: t.Error}})
}

func (w *worker) post(ctx context.Context, request *wire.Frame) {
	if w.native == nil || !validPost(request) {
		w.peer.reply(request, &wire.Frame{Error: protocolError})
		return
	}
	reply, err := w.native.PostAuthorized(ctx, request.Path, shared.Authorization{Scheme: "DPoP", Token: request.Token, Proof: request.Proof}, request.ContentType, request.Body)
	w.peer.reply(request, &wire.Frame{Status: int32(reply.Status), Nonce: reply.Nonce, Error: errorCode(err)})
}

func validPost(request *wire.Frame) bool {
	return (request.Path == protocol.BindPath || request.Path == protocol.MigrationPath) && request.ContentType == "application/protobuf" && len(request.Body) <= protocol.MaxFrameBytes
}

func (w *worker) dial(request *wire.Frame, stream *workerStream) {
	socket, reply, err := w.upgrade(stream, request)
	if err != nil {
		w.finishStream(stream)
		w.peer.reply(request, &wire.Frame{Status: int32(reply.Status), Nonce: reply.Nonce, Error: errorCode(err)})
		return
	}
	if !w.attachSocket(stream, socket) {
		_ = socket.Close()
		w.finishStream(stream)
		w.peer.reply(request, &wire.Frame{Error: closedError})
		return
	}
	w.peer.reply(request, &wire.Frame{Status: int32(reply.Status), Nonce: reply.Nonce})
	w.wg.Add(1)
	go w.receive(stream)
}

func (w *worker) upgrade(stream *workerStream, request *wire.Frame) (*shared.Conn, shared.Reply, error) {
	// Cancelling the attempt cancels this reservation, but a successful upgrade
	// retires its timer without cancelling the live connection's context.
	deadline := time.AfterFunc(5*time.Second, stream.cancel)
	socket, reply, err := w.native.DialAuthorized(stream.ctx, request.Path, shared.Authorization{Scheme: "DPoP", Token: request.Token, Proof: request.Proof})
	stopped := deadline.Stop()
	if err != nil {
		return nil, reply, err
	}
	if !stopped || stream.ctx.Err() != nil {
		_ = socket.Close()
		return nil, reply, context.DeadlineExceeded
	}
	return socket, reply, nil
}

func (w *worker) reserveStream(id uint32) *workerStream {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.peer.ctx.Err() != nil {
		return nil
	}
	for _, stream := range w.streams {
		if stream != nil && stream.id == id {
			return nil
		}
	}
	for i := range w.streams {
		if w.streams[i] != nil {
			continue
		}
		ctx, cancel := context.WithCancel(w.peer.ctx)
		stream := &workerStream{id: id, ctx: ctx, ack: make(chan struct{}, 1), cancel: cancel}
		w.streams[i] = stream
		return stream
	}
	return nil
}

func (w *worker) attachSocket(stream *workerStream, socket *shared.Conn) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || stream.ctx.Err() != nil {
		return false
	}
	for _, owned := range w.streams {
		if owned == stream {
			stream.socket = socket
			return true
		}
	}
	return false
}

func (w *worker) receive(stream *workerStream) {
	defer w.wg.Done()
	defer w.finishStream(stream)
	message := protocol.ServerMessageFromVTPool()
	defer message.ReturnToVTPool()
	var buffer [protocol.MaxFrameBytes]byte
	for {
		if err := readStreamMessage(stream, buffer[:], message); err != nil {
			if stream.ctx.Err() == nil {
				_ = w.peer.send.write(&wire.Frame{Kind: streamError, Stream: stream.id, Error: errorCode(err)})
			}
			return
		}
		if w.peer.send.write(&wire.Frame{Kind: streamFrame, Stream: stream.id, Message: message}) != nil {
			return
		}
		select {
		case <-stream.ack:
		case <-stream.ctx.Done():
			return
		}
	}
}

func readStreamMessage(stream *workerStream, buffer []byte, message *protocol.Message) error {
	size, err := stream.socket.Read(stream.ctx, buffer)
	if err != nil {
		return err
	}
	if err = protobuf.Unmarshal(buffer[:size], message); err != nil {
		return err
	}
	return protocol.Validate(message)
}

func (w *worker) stopStream(id uint32) {
	w.mu.Lock()
	var target *workerStream
	for _, stream := range w.streams {
		if stream != nil && stream.id == id {
			target = stream
			break
		}
	}
	w.mu.Unlock()
	if target != nil {
		target.cancel()
	}
}

// A cancelled slot remains reserved until its existing request/receive owner has
// closed the socket. At most two sockets, including closing sockets, are owned;
// repeated handoffs cannot create an unbounded set of close goroutines.
func (w *worker) finishStream(stream *workerStream) {
	stream.cancel()
	w.mu.Lock()
	socket := stream.socket
	w.mu.Unlock()
	if socket != nil {
		_ = socket.Close()
	}
	w.mu.Lock()
	for i, owned := range w.streams {
		if owned == stream {
			w.streams[i] = nil
			break
		}
	}
	w.mu.Unlock()
}

func (w *worker) acknowledge(id uint32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, stream := range w.streams {
		if stream == nil || stream.id != id {
			continue
		}
		select {
		case stream.ack <- struct{}{}:
		default:
			w.peer.cancel()
		}
		return
	}
}

func (w *worker) close() {
	w.closeOnce.Do(func() {
		w.peer.cancel()
		w.closeIO()
		w.mu.Lock()
		w.closed = true
		for _, stream := range w.streams {
			if stream != nil {
				stream.cancel()
			}
		}
		for _, operation := range w.operations {
			if operation != nil {
				operation.cancel()
			}
		}
		w.mu.Unlock()
		closeFiles(w.files)
		if w.native != nil {
			_ = w.native.Close()
		}
		if w.oauth != nil {
			_ = w.oauth.Close()
		}
		w.wg.Wait()
	})
}
