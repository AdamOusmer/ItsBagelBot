// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type LivePollOptions = {
  firstDelayMs: number;
  delayMs: (elapsedMs: number) => number;
  timeoutMs: number;
  onDone?: () => void;
  now?: () => number;
};

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
