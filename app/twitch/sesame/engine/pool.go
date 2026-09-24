// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"sync"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
)

var ctxPool = sync.Pool{New: func() any { return new(module.Context) }}

func GetContext() *module.Context {
	c := ctxPool.Get().(*module.Context)
	c.Reset()
	return c
}

func PutContext(c *module.Context) {
	if c == nil {
		return
	}
	c.Reset()
	ctxPool.Put(c)
}

var envPool = sync.Pool{New: func() any { return new(lane.Envelope) }}

func GetEnvelope() *lane.Envelope {
	e := envPool.Get().(*lane.Envelope)
	resetEnvelope(e)
	return e
}

func PutEnvelope(e *lane.Envelope) {
	if e == nil {
		return
	}
	resetEnvelope(e)
	envPool.Put(e)
}

func resetEnvelope(e *lane.Envelope) {
	badges := e.Badges[:0]
	*e = lane.Envelope{}
	e.Badges = badges
}

var outPool = sync.Pool{New: func() any { return new(module.Output) }}

func GetOutput() *module.Output {
	o := outPool.Get().(*module.Output)
	*o = module.Output{}
	return o
}

func PutOutput(o *module.Output) {
	if o == nil {
		return
	}
	*o = module.Output{}
	outPool.Put(o)
}

var bufPool = sync.Pool{New: func() any { b := make([]byte, 0, 256); return &b }}

func GetBuf() []byte {
	bp := bufPool.Get().(*[]byte)
	return (*bp)[:0]
}

func PutBuf(b []byte) {
	b = b[:0]
	bufPool.Put(&b)
}
