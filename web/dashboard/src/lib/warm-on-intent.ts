// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const CONTROLS = 'button, a[href], input, select, [role="button"], [role="option"], [role="row"]';
const IDLE_FALLBACK_MS = 1500;

type Loader = () => Promise<unknown>;

function saveDataOn(): boolean {
  return (navigator as Navigator & { connection?: { saveData?: boolean } }).connection?.saveData === true;
}

function whenIdle(run: () => void): () => void {
  if (saveDataOn()) return () => {};
  let cancel = () => {};
  const schedule = () => {
    if (typeof requestIdleCallback === 'function') {
      const id = requestIdleCallback(run);
      cancel = () => cancelIdleCallback(id);
      return;
    }
    const id = setTimeout(run, IDLE_FALLBACK_MS);
    cancel = () => clearTimeout(id);
  };
  if (document.readyState === 'complete') schedule();
  else window.addEventListener('load', schedule, { once: true });
  return () => {
    window.removeEventListener('load', schedule);
    cancel();
  };
}

export function warmOnIntent(node: HTMLElement, loaders: ReadonlyArray<Loader>) {
  let warmed = false;
  const warm = () => {
    if (warmed) return;
    warmed = true;
    stop();
    for (const load of loaders) void load();
  };
  const onIntent = (event: Event) => {
    if (event.target instanceof Element && event.target.closest(CONTROLS)) warm();
  };
  const stopIdle = whenIdle(warm);
  const stop = () => {
    node.removeEventListener('pointerover', onIntent);
    node.removeEventListener('focusin', onIntent);
    stopIdle();
  };
  node.addEventListener('pointerover', onIntent, { passive: true });
  node.addEventListener('focusin', onIntent);
  return { destroy: stop };
}
