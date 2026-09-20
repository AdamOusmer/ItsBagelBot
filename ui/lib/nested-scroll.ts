// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Which element keeps the wheel: the page, or a scroll pane under the pointer.
 *
 * The smooth scroller preventDefaults every wheel event it accepts, so a nested
 * `overflow: auto` pane (the console's inspector, a picker list, the rail, a
 * textarea) never scrolls natively unless something tells Lenis to stand back.
 * Until 2026-09-18 that something was a `data-lenis-prevent` on each pane. It
 * is static: it also stands back when the pane has nothing to scroll, the
 * wheel then lands on a pane that cannot move, and with the pane's
 * `overscroll-behavior: contain` it ends there. Over a short rehearsal the page
 * froze under the pointer.
 *
 * Lenis 1.3 ships `allowNestedScroll` for exactly this, and its shape is the
 * right one: yield while the pane can move in the wheel's direction, take the
 * page again at its edges. It is not used here because its `hasNestedScroll`
 * caches overflow, scrollHeight and clientHeight PER ELEMENT FOR 2000 ms
 * (lenis@1.3.26 dist/lenis.mjs, the `node._lenis` cache), and the pane it is
 * meant for changes height on every click: expand a row, add a line, open a
 * field. Measured 2026-09-20 in headless Chromium 153 with a sticky
 * `overflow-y: auto` pane and real wheel input: grow the pane's content within
 * 2 s of the last wheel over it and the next wheel scrolls the PAGE while the
 * pane sits still; shrink it and the wheel goes native into a pane that
 * cannot move. Both recover after the cache expires, which is what made them
 * look like flakiness rather than a rule.
 *
 * This is the same predicate with the cache removed: one getComputedStyle and,
 * only when the overflow says the element scrolls, three property reads, per
 * ancestor per wheel event. A wheel burst is ~120 events/s over a path of
 * 10-20 elements, and Lenis forces a layout each frame regardless, so the cost
 * does not register.
 *
 * It mirrors Lenis's own rules so the two cannot disagree on semantics: the
 * dominant axis of the gesture is the one asked about, an `overscroll-behavior`
 * other than `auto` keeps the wheel in the pane at its edges too (that is what
 * `contain` means), and positions are rounded the way Lenis rounds them.
 */

/** One scroll axis of an element: everything the rule reads, in one direction. */
export interface ScrollAxis {
  overflow: string;
  overscroll: string;
  /** scrollLeft or scrollTop. */
  at: number;
  /** The last reachable `at`: scrollWidth − clientWidth, or the same in height. */
  max: number;
}

/** The reads the decision is made from, so the rule is testable without a DOM. */
export interface ScrollBox {
  x: ScrollAxis;
  y: ScrollAxis;
}

export interface Gesture {
  deltaX: number;
  deltaY: number;
}

/** What Lenis hands to `virtualScroll`; only the fields read here are typed. */
export interface VirtualScrollData extends Gesture {
  event: { type: string };
}

const SCROLLS = new Set(['auto', 'scroll', 'overlay']);

/** The axis the gesture is mostly along, and its delta. Ties go to horizontal, as in Lenis. */
function along(box: ScrollBox, { deltaX, deltaY }: Gesture): { axis: ScrollAxis; delta: number } {
  return Math.abs(deltaX) >= Math.abs(deltaY)
    ? { axis: box.x, delta: deltaX }
    : { axis: box.y, delta: deltaY };
}

/** True when the box takes this gesture itself; false hands it to the page. */
export function keepsWheel(box: ScrollBox, gesture: Gesture): boolean {
  const { axis, delta } = along(box, gesture);
  if (!SCROLLS.has(axis.overflow) || axis.max <= 0) return false;
  if (axis.overscroll !== 'auto') return true;
  const at = Math.round(axis.at);
  return delta > 0 ? at < axis.max : at > 0;
}

/**
 * Reads the box live. Geometry is read only when the computed overflow can
 * scroll: scrollHeight forces layout, the overflow check does not, and most
 * elements on a wheel's path are not panes.
 */
export function readScrollBox(node: Element): ScrollBox {
  const style = getComputedStyle(node);
  const x: ScrollAxis = { overflow: style.overflowX, overscroll: style.overscrollBehaviorX, at: 0, max: 0 };
  const y: ScrollAxis = { overflow: style.overflowY, overscroll: style.overscrollBehaviorY, at: 0, max: 0 };
  if (SCROLLS.has(x.overflow) || SCROLLS.has(y.overflow)) {
    x.at = node.scrollLeft;
    x.max = node.scrollWidth - node.clientWidth;
    y.at = node.scrollTop;
    y.max = node.scrollHeight - node.clientHeight;
  }
  return { x, y };
}

/**
 * The two Lenis options the gate occupies, and the caller's own to wrap. The
 * data type is generic so a caller typed against Lenis's `VirtualScrollData`
 * (a real WheelEvent | TouchEvent) fits without this module importing it.
 */
export interface NestedScrollHooks<D extends VirtualScrollData = VirtualScrollData> {
  prevent?: (node: HTMLElement) => boolean;
  virtualScroll?: (data: D) => boolean | void;
}

export interface NestedScrollGate<D extends VirtualScrollData = VirtualScrollData> {
  /**
   * Pass as Lenis's `virtualScroll`. Returns false exactly when the wrapped
   * one did, which is the one value Lenis acts on (it drops the gesture).
   */
  virtualScroll: (data: D) => boolean;
  /** Pass as Lenis's `prevent`. The wrapped one is asked first. */
  prevent: (node: HTMLElement) => boolean;
}

/**
 * Binds the predicate to a Lenis instance through its public options. Lenis
 * runs `virtualScroll` on every gesture BEFORE it walks the elements under the
 * pointer asking `prevent`, so the gesture is parked here for that walk; there
 * is no other channel, `prevent` receives only the element. Touch is left to
 * the browser: `syncTouch` is off on every surface, so a parked touch gesture
 * would only ever answer a question Lenis is not going to act on.
 */
export function createNestedScrollGate<D extends VirtualScrollData = VirtualScrollData>(
  outer: NestedScrollHooks<D> = {},
): NestedScrollGate<D> {
  let wheel: Gesture | null = null;
  return {
    virtualScroll(data) {
      wheel = data.event.type === 'wheel' ? data : null;
      return outer.virtualScroll?.(data) !== false;
    },
    prevent(node) {
      if (outer.prevent?.(node)) return true;
      return wheel !== null && keepsWheel(readScrollBox(node), wheel);
    },
  };
}
