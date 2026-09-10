// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Resolves `p`, or `fallback` when it rejects: the shell's best-effort read.
 *
 * Every console layout load is a fan-out of independent reads, and none of them
 * may take the page down with it -- a blipped notifications RPC must cost the
 * bell, not the whole shell. Spelled inline that is a `.catch(() => x)` tail on
 * every call, which reads as an afterthought and was twice written as a bare
 * `.catch(() => {})` beside a mutable `let`, quietly leaving the previous
 * value in place instead of degrading to a stated default.
 *
 * Named on purpose: the fallback is the degraded contract, so it is worth being
 * able to see it at the call site. This swallows the rejection rather than
 * logging it because the transport layer already logs the failed RPC; a second
 * log here only doubles the noise of one outage.
 */
export function bestEffort<T>(p: Promise<T>, fallback: T): Promise<T> {
  return p.catch(() => fallback);
}
