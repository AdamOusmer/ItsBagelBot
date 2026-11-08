// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/network/wire"
)

func TestPeerPendingCallsAreStrictlyBounded(t *testing.T) {
	p := newPeer(t.Context(), strings.NewReader(""), io.Discard)
	var ids [requestCount]uint64
	for i := range ids {
		id, _, err := p.reserve(oauthRequest)
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	if _, _, err := p.reserve(oauthRequest); !errors.Is(err, ErrWorker) {
		t.Fatal("private RPC queue grew beyond fixed capacity")
	}
	for _, id := range ids {
		p.release(id)
	}
	if _, _, err := p.reserve(oauthRequest); err != nil {
		t.Fatal("released RPC capacity was not reusable")
	}
}

func TestDuplicatePeerResponseFailsClosedAndReleasesQueuedLease(t *testing.T) {
	p := newPeer(t.Context(), strings.NewReader(""), io.Discard)
	id, queue, err := p.reserve(oauthRequest)
	if err != nil {
		t.Fatal(err)
	}
	first := wire.FrameFromVTPool()
	first.Kind = response
	first.Id = id
	first.Oauth = &wire.OAuthReply{Refresh: "retained-once"}
	if !p.deliver(first) {
		first.ReturnToVTPool()
		t.Fatal("first response was refused")
	}
	duplicate := wire.FrameFromVTPool()
	duplicate.Kind = response
	duplicate.Id = id
	if p.deliver(duplicate) {
		t.Fatal("duplicate response retained a second lease")
	}
	duplicate.ReturnToVTPool()
	p.release(id)
	if p.ctx.Err() == nil || len(queue) != 0 {
		t.Fatal("duplicate response did not stop peer and drain its outstanding response")
	}
}

func TestPeerLateResponseCannotSatisfyNewCall(t *testing.T) {
	p := newPeer(t.Context(), strings.NewReader(""), io.Discard)
	old, _, err := p.reserve(oauthRequest)
	if err != nil {
		t.Fatal(err)
	}
	p.release(old)
	current, queue, err := p.reserve(oauthRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer p.release(current)
	late := wire.FrameFromVTPool()
	late.Kind = response
	late.Id = old
	if p.deliver(late) {
		t.Fatal("cancelled RPC response crossed into a new call")
	}
	late.ReturnToVTPool()
	if current == old || len(queue) != 0 {
		t.Fatal("private RPC generation was reused")
	}
}

func TestPeerCancelledCallsPublishNoCredentials(t *testing.T) {
	for _, cancelPeer := range []bool{false, true} {
		var output bytes.Buffer
		p := newPeer(t.Context(), strings.NewReader(""), &output)
		ctx, cancel := context.WithCancel(t.Context())
		if cancelPeer {
			p.cancel()
		} else {
			cancel()
		}
		_, err := p.call(ctx, &wire.Frame{Kind: oauthRequest, Form: "refresh_token=private-value"})
		cancel()
		if err == nil || output.Len() != 0 {
			t.Fatal("cancelled private RPC still published credential-bearing work")
		}
	}
}

func TestPeerCancellationQueueHasFixedCapacity(t *testing.T) {
	p := newPeer(t.Context(), strings.NewReader(""), io.Discard)
	for i := 0; i < requestCount; i++ {
		p.cancelCall(uint64(i + 1))
	}
	if p.ctx.Err() != nil {
		t.Fatal("bounded cancellation traffic stopped peer")
	}
	p.cancelCall(requestCount + 1)
	if p.ctx.Err() == nil || len(p.cancels) != requestCount {
		t.Fatal("cancellation queue grew instead of failing closed")
	}
}

func TestPeerContextCancellationInterruptsBlockedPipeReaderAndJoinsHelpers(t *testing.T) {
	input, writer := io.Pipe()
	output, reader := io.Pipe()
	p := newPeer(t.Context(), input, reader)
	go p.run()
	p.cancel()
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("private peer cancellation did not release its reader and helper owners")
	}
	_ = writer.Close()
	_ = output.Close()
}
