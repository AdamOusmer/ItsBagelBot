// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type LivePollOptions = {
  firstDelayMs: number;
  delayMs: (elapsedMs: number) => number;
  timeoutMs: number;
  onDone?: () => void;
  now?: () => number;
  hiddenDelayMs?: number;
  refreshOnVisible?: boolean;
};

const NOOP = () => {};

function pageHidden(): boolean {
  return typeof document !== 'undefined' && document.hidden;
}

function watchVisibility(onVisible: (() => void) | null): () => void {
  if (!onVisible || typeof document === 'undefined') return NOOP;
  document.addEventListener('visibilitychange', onVisible);
  return () => document.removeEventListener('visibilitychange', onVisible);
}

function nextDelay(opts: LivePollOptions, elapsedMs: number): number {
  if (opts.hiddenDelayMs !== undefined && pageHidden()) return opts.hiddenDelayMs;
  return opts.delayMs(elapsedMs);
}

export function livePoll(
  tick: (elapsedMs: number) => Promise<boolean>,
  opts: LivePollOptions
): () => void {
  const clock = opts.now ?? Date.now;
  const started = clock();
  let timer: ReturnType<typeof setTimeout> | null = null;
  let live = true;
  let inFlight = false;

  const clearTimer = () => {
    if (timer) clearTimeout(timer);
    timer = null;
  };

  const finish = () => {
    if (!live) return;
    live = false;
    clearTimer();
    stopWatching();
    opts.onDone?.();
  };

  const run = async () => {
    timer = null;
    inFlight = true;
    let settled: boolean;
    try {
      settled = await tick(clock() - started);
    } finally {
      inFlight = false;
    }
    if (!live) return;
    const elapsed = clock() - started;
    if (settled || elapsed >= opts.timeoutMs) return finish();
    timer = setTimeout(run, nextDelay(opts, elapsed));
  };

  const refresh = () => {
    if (!live || inFlight || pageHidden()) return;
    clearTimer();
    void run();
  };

  const stopWatching = watchVisibility(opts.refreshOnVisible ? refresh : null);
  timer = setTimeout(run, opts.firstDelayMs);
  return finish;
}
