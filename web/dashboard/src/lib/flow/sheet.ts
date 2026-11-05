// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
export const SHEET_QUERY = '(max-width: 760px), (pointer: coarse) and (max-width: 1023px)';

const SETTLE_MS = 180;
const FLICK_PX_PER_MS = 0.5;
const FLICK_MIN_PX = 24;
const DISTANCE_SHARE = 0.3;

export function swipeDismisses(dy: number, ms: number, sheetHeight: number): boolean {
  if (dy >= sheetHeight * DISTANCE_SHARE) return true;
  return dy >= FLICK_MIN_PX && dy / Math.max(1, ms) >= FLICK_PX_PER_MS;
}

export function sheetDrag(el: HTMLElement, initial: () => void) {
  let onDismiss = initial;
  let drag: { id: number; y: number; t: number; dy: number } | null = null;

  const settle = (to: string, then?: () => void) => {
    const still = matchMedia('(prefers-reduced-motion: reduce)').matches;
    el.style.transition = still ? '' : `transform ${SETTLE_MS}ms cubic-bezier(0.16, 1, 0.3, 1)`;
    el.style.transform = to;
    setTimeout(
      () => {
        el.style.transition = '';
        el.style.transform = '';
        then?.();
      },
      still ? 0 : SETTLE_MS
    );
  };

  const down = (e: PointerEvent) => {
    if (drag || !matchMedia(SHEET_QUERY).matches) return;
    const target = e.target as Element;
    if (!target.closest('header, [data-sheet-grip]') || target.closest('button, a, input, textarea, select')) return;
    drag = { id: e.pointerId, y: e.clientY, t: e.timeStamp, dy: 0 };
    el.setPointerCapture(e.pointerId);
    el.style.transition = '';
  };

  const move = (e: PointerEvent) => {
    if (e.pointerId !== drag?.id) return;
    drag.dy = Math.max(0, e.clientY - drag.y);
    el.style.transform = `translateY(${drag.dy}px)`;
  };

  const end = (e: PointerEvent) => {
    if (e.pointerId !== drag?.id) return;
    const { dy, t } = drag;
    drag = null;
    if (e.type === 'pointerup' && swipeDismisses(dy, e.timeStamp - t, el.offsetHeight)) settle('translateY(100%)', () => onDismiss());
    else settle('translateY(0)');
  };

  el.addEventListener('pointerdown', down);
  el.addEventListener('pointermove', move);
  el.addEventListener('pointerup', end);
  el.addEventListener('pointercancel', end);
  return {
    update(next: () => void) {
      onDismiss = next;
    },
    destroy() {
      el.removeEventListener('pointerdown', down);
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', end);
      el.removeEventListener('pointercancel', end);
    }
  };
}
