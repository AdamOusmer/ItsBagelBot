// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const clocks = new Set<() => void>();
let timer: ReturnType<typeof setInterval> | undefined;

function tickAll(): void {
  for (const tick of clocks) tick();
}

function start(): void {
  if (timer !== undefined) return;
  timer = setInterval(tickAll, 1000);
}

function stop(): void {
  if (timer === undefined) return;
  clearInterval(timer);
  timer = undefined;
}

export interface ClockOptions {
  hour12?: boolean;
  locale?: string;
}

export function mountClock(el: HTMLElement, options: ClockOptions = {}): () => void {
  const { hour12 = false, locale } = options;
  const tick = () => {
    el.textContent = new Date().toLocaleTimeString(locale, { hour12 });
  };

  tick();
  clocks.add(tick);
  start();

  return () => {
    clocks.delete(tick);
    if (clocks.size === 0) stop();
  };
}
