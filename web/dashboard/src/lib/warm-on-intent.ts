// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const CONTROLS = 'button, a[href], input, select, [role="button"], [role="option"], [role="row"]';

export function warmOnIntent(node: HTMLElement, loaders: ReadonlyArray<() => Promise<unknown>>) {
  const warm = (event: Event) => {
    if (!(event.target instanceof Element) || !event.target.closest(CONTROLS)) return;
    stop();
    for (const load of loaders) void load();
  };
  const stop = () => {
    node.removeEventListener('pointerover', warm);
    node.removeEventListener('focusin', warm);
  };
  node.addEventListener('pointerover', warm, { passive: true });
  node.addEventListener('focusin', warm);
  return { destroy: stop };
}
