// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const HIDDEN_GRACE_MS = 30_000;
export const IDLE_MS = 15 * 60_000;

const ACTIVITY = ['pointerdown', 'pointermove', 'keydown', 'wheel', 'touchstart', 'focus'];

type Listen = Pick<EventTarget, 'addEventListener' | 'removeEventListener'>;

export interface StreamHost {
  doc: Listen & { readonly hidden: boolean };
  win: Listen;
  connect(url: string): EventSource;
}

const browserHost = (): StreamHost => ({
  doc: document,
  win: window,
  connect: (url) => new EventSource(url)
});

export function visibleEventSource(
  url: string,
  attach: (es: EventSource, reopened: boolean) => void,
  host: StreamHost = browserHost()
): () => void {
  let es: EventSource | null = null;
  let opened = false;
  let hiddenTimer: ReturnType<typeof setTimeout> | undefined;
  let idleTimer: ReturnType<typeof setTimeout> | undefined;
  let lastInput = 0;

  const open = () => {
    if (es) return;
    es = host.connect(url);
    attach(es, opened);
    opened = true;
  };
  const close = () => {
    clearTimeout(hiddenTimer);
    hiddenTimer = undefined;
    es?.close();
    es = null;
  };
  const checkIdle = () => {
    const quiet = performance.now() - lastInput;
    if (quiet < IDLE_MS) {
      idleTimer = setTimeout(checkIdle, IDLE_MS - quiet);
      return;
    }
    idleTimer = undefined;
    close();
  };
  const wake = () => {
    if (host.doc.hidden) return;
    lastInput = performance.now();
    clearTimeout(hiddenTimer);
    hiddenTimer = undefined;
    idleTimer ??= setTimeout(checkIdle, IDLE_MS);
    open();
  };
  const onVisibility = () => {
    if (!host.doc.hidden) return wake();
    if (es) hiddenTimer ??= setTimeout(close, HIDDEN_GRACE_MS);
  };

  host.doc.addEventListener('visibilitychange', onVisibility);
  host.win.addEventListener('pagehide', close);
  host.win.addEventListener('pageshow', wake);
  for (const type of ACTIVITY) host.win.addEventListener(type, wake, { passive: true });
  wake();

  return () => {
    host.doc.removeEventListener('visibilitychange', onVisibility);
    host.win.removeEventListener('pagehide', close);
    host.win.removeEventListener('pageshow', wake);
    for (const type of ACTIVITY) host.win.removeEventListener(type, wake);
    clearTimeout(idleTimer);
    close();
  };
}
