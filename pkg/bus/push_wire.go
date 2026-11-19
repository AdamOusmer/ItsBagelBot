// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

type pushWire struct {
	msg *nats.Msg
	nc  *nats.Conn
}

func (w pushWire) Data() []byte         { return w.msg.Data }
func (w pushWire) Headers() nats.Header { return w.msg.Header }
func (w pushWire) Subject() string      { return w.msg.Subject }
func (w pushWire) Reply() string        { return w.msg.Reply }

func (w pushWire) Metadata() (*jsapi.MsgMetadata, error) {
	m, err := w.msg.Metadata()
	if err != nil {
		return nil, err
	}
	return &jsapi.MsgMetadata{
		Sequence:     jsapi.SequencePair{Consumer: m.Sequence.Consumer, Stream: m.Sequence.Stream},
		NumDelivered: m.NumDelivered, NumPending: m.NumPending, Timestamp: m.Timestamp,
		Stream: m.Stream, Consumer: m.Consumer, Domain: m.Domain,
	}, nil
}

func (w pushWire) ack(body string) error { return w.nc.Publish(w.Reply(), []byte(body)) }

func (w pushWire) Ack() error        { return w.ack("+ACK") }
func (w pushWire) Nak() error        { return w.ack("-NAK") }
func (w pushWire) InProgress() error { return w.ack("+WPI") }
func (w pushWire) Term() error       { return w.ack("+TERM") }

func (w pushWire) DoubleAck(ctx context.Context) error {
	_, err := w.nc.RequestWithContext(ctx, w.Reply(), []byte("+ACK"))
	return err
}

func (w pushWire) NakWithDelay(delay time.Duration) error {
	return w.ack(`-NAK {"delay":` + strconv.FormatInt(delay.Nanoseconds(), 10) + "}")
}

func (w pushWire) TermWithReason(reason string) error {
	return w.ack("+TERM " + reason)
}
