// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
export const SHEET_QUERY = '(max-width: 760px), (pointer: coarse) and (max-width: 1023px)';

export const SHEET_MIN = 280;
const SETTLE_MS = 180;
const FLICK_PX_PER_MS = 0.5;
const FLICK_MIN_PX = 24;
const DISTANCE_SHARE = 0.3;

export function swipeDismisses(dy: number, ms: number, sheetHeight: number): boolean {
  if (dy >= sheetHeight * DISTANCE_SHARE) return true;
  return dy >= FLICK_MIN_PX && dy / Math.max(1, ms) >= FLICK_PX_PER_MS;
}

export type Span = { top: number; bottom: number };

const TEXT_FIELD = 'input, textarea, select, [contenteditable=true]';

export function fieldScrollDelta(field: Span, view: Span): number {
  if (field.top >= view.top && field.bottom <= view.bottom) return 0;
  return (field.top + field.bottom) / 2 - (view.top + view.bottom) / 2;
}

export function editingInSheet(): boolean {
  const field = document.activeElement;
  return Boolean(field?.matches(TEXT_FIELD) && field.closest('.bb-editor-stage__panel'));
}

export function revealFocusedField(viewport: Pick<VisualViewport, 'height' | 'offsetTop'> | null) {
  const field = document.activeElement;
  const scroller = editingInSheet() ? field?.closest<HTMLElement>('.scroll, .bb-scroller') : null;
  if (!field || !scroller) return;
  const box = scroller.getBoundingClientRect();
  const top = viewport?.offsetTop ?? 0;
  const view = { top: Math.max(box.top, top), bottom: Math.min(box.bottom, top + (viewport?.height ?? innerHeight)) };
  scroller.scrollTop += fieldScrollDelta(field.getBoundingClientRect(), view);
}

export function sheetDrag(el: HTMLElement, initial: () => void, onResize: (height: number) => void = () => {}) {
  let onDismiss = initial;
  let drag: { id: number; y: number; t: number; dy: number; height: number; limit: number; size: number } | null = null;

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
    if (!target.closest('header, .bb-editor-stage__grip') || target.closest('button, a, input, textarea, select')) return;
    const stage = el.closest('.bb-editor-stage');
    const limit = parseFloat(stage ? getComputedStyle(stage).getPropertyValue('--stage-sheet-limit') : '') || innerHeight - 64;
    drag = { id: e.pointerId, y: e.clientY, t: e.timeStamp, dy: 0, height: el.offsetHeight, limit, size: el.offsetHeight };
    el.setPointerCapture(e.pointerId);
    el.style.transition = '';
  };

  const move = (e: PointerEvent) => {
    if (e.pointerId !== drag?.id) return;
    drag.dy = e.clientY - drag.y;
    drag.size = Math.min(drag.limit, Math.max(160, drag.height - drag.dy));
    onResize(drag.size);
  };

  const end = (e: PointerEvent) => {
    if (e.pointerId !== drag?.id) return;
    const { dy, t, height, size } = drag;
    drag = null;
    if (e.type === 'pointerup' && swipeDismisses(dy, e.timeStamp - t, height)) return settle('translateY(100%)', () => onDismiss());
    if (size < SHEET_MIN) onResize(SHEET_MIN);
    settle('translateY(0)');
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
