package engine

import (
	"sync"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
)

// The pipeline runs one Context, one Envelope, a handful of Outputs and a
// scratch buffer per message on a hot path that fires for every chat line, so
// each of those is pooled here. The pipeline owns the lifecycle: Get one, fill
// it, use it within the message, then Put it back. Nothing pooled may be
// retained past the message it was Got for.

var ctxPool = sync.Pool{New: func() any { return new(module.Context) }}

// GetContext returns a zeroed *module.Context from the pool, ready to fill.
func GetContext() *module.Context {
	c := ctxPool.Get().(*module.Context)
	c.Reset()
	return c
}

// PutContext returns a Context to the pool. The caller must not use c again.
func PutContext(c *module.Context) {
	if c == nil {
		return
	}
	c.Reset()
	ctxPool.Put(c)
}

var envPool = sync.Pool{New: func() any { return new(lane.Envelope) }}

// GetEnvelope returns a reset *lane.Envelope from the pool. The Badges backing
// array is retained (truncated to len 0) so decoding into it reuses capacity.
func GetEnvelope() *lane.Envelope {
	e := envPool.Get().(*lane.Envelope)
	resetEnvelope(e)
	return e
}

// PutEnvelope returns an Envelope to the pool. The caller must not use e again.
func PutEnvelope(e *lane.Envelope) {
	if e == nil {
		return
	}
	resetEnvelope(e)
	envPool.Put(e)
}

// resetEnvelope zeroes an Envelope's fields while keeping the Badges backing
// array so it can be re-decoded into without a fresh allocation.
func resetEnvelope(e *lane.Envelope) {
	badges := e.Badges[:0]
	*e = lane.Envelope{}
	e.Badges = badges
}

var outPool = sync.Pool{New: func() any { return new(module.Output) }}

// GetOutput returns a zeroed *module.Output from the pool, ready to fill.
func GetOutput() *module.Output {
	o := outPool.Get().(*module.Output)
	*o = module.Output{}
	return o
}

// PutOutput returns an Output to the pool. The caller must not use o again.
func PutOutput(o *module.Output) {
	if o == nil {
		return
	}
	*o = module.Output{}
	outPool.Put(o)
}

// bufPool hands out []byte scratch buffers for the zero-alloc template/key
// builders. Buffers come back reset to length 0 with their capacity intact.
var bufPool = sync.Pool{New: func() any { b := make([]byte, 0, 256); return &b }}

// GetBuf returns a scratch buffer with len 0 (capacity retained).
func GetBuf() []byte {
	bp := bufPool.Get().(*[]byte)
	return (*bp)[:0]
}

// PutBuf returns a scratch buffer to the pool. The caller must not use b again.
func PutBuf(b []byte) {
	b = b[:0]
	bufPool.Put(&b)
}
