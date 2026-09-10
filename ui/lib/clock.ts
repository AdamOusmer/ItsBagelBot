// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The strip's wall clock: a local time readout, ticking once a second.
//
// ONE interval for every clock on the page, not one per element. It was a
// per-instance `setInterval` inside the topbar component, which is invisible
// while a page renders one topbar and becomes a real cost the moment a guide
// screen or a preview renders several -- n timers, each waking the tab
// independently, each firing at its own phase, so two clocks a few hundred
// milliseconds apart show different seconds side by side. The shared interval
// also means every clock repaints on the SAME tick.
//
// Not on the rAF loop, deliberately: this updates 1/s, not 60/s, and rAF is
// suspended in a hidden tab (which is correct for animation and wrong for a
// clock -- it would show a stale time for as long as the tab was in the
// background and then jump).

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
  /** 24-hour by default: the console's strip is a control room readout. */
  hour12?: boolean;
  /** Locale for the format. Undefined = the browser's. */
  locale?: string;
}

/**
 * Write the current time into `el` and keep it current. Returns `dispose()`,
 * which drops this clock and clears the shared interval when it was the last.
 */
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
