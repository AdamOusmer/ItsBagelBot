// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The generic half of "poll a backend until it settles": the timer loop, the
// backoff schedule, the deadline, and the generation counter that makes a
// second start cancel the first.
//
// It exists because that loop was written inline on the dashboard's Overview
// page and is about to be written a second time in the admin console. The
// interesting part of such a poll is never the loop; it is the predicate that
// says "settled", which stays with the caller. Everything below is bookkeeping
// that is wrong in the same three ways every time it is retyped: an in-flight
// await resolving after a stop and writing to dead state, a missing ceiling
// that polls forever when the backend never settles, and a restart that leaves
// the previous timer running so two loops interleave.
//
// The generation counter, not a boolean flag, is what makes the first of those
// safe: `stop()` bumps the generation, so a tick that was awaiting a fetch when
// it happened sees a stale generation on resume and returns without scheduling
// or reporting anything.

export type LivePollOptions = {
  /** Delay before the first tick. */
  firstDelayMs: number;
  /** Delay before the next tick, given how long the poll has been running. */
  delayMs: (elapsedMs: number) => number;
  /** Hard ceiling: the poll gives up here even if nothing ever settled. */
  timeoutMs: number;
  /** Runs once when the poll settles, times out, or is stopped by the caller. */
  onDone?: () => void;
  /** Injectable clock, for tests. */
  now?: () => number;
};

/**
 * Runs `tick` on a backoff schedule until it reports settled or the deadline
 * passes, and returns the stop handle.
 *
 * `tick` receives the elapsed milliseconds and returns whether the poll is
 * done. It is never called concurrently with itself: the next timer is only
 * armed after the previous tick's promise resolves.
 *
 * Calling the returned stop handle is idempotent and safe from a component
 * teardown; `onDone` runs at most once, whichever way the poll ends.
 */
export function livePoll(
  tick: (elapsedMs: number) => Promise<boolean>,
  opts: LivePollOptions
): () => void {
  const clock = opts.now ?? Date.now;
  const started = clock();
  let timer: ReturnType<typeof setTimeout> | null = null;
  let live = true;

  const finish = () => {
    if (!live) return;
    live = false;
    if (timer) clearTimeout(timer);
    timer = null;
    opts.onDone?.();
  };

  const run = async () => {
    const settled = await tick(clock() - started);
    if (!live) return;
    const elapsed = clock() - started;
    if (settled || elapsed >= opts.timeoutMs) return finish();
    timer = setTimeout(run, opts.delayMs(elapsed));
  };

  timer = setTimeout(run, opts.firstDelayMs);
  return finish;
}
