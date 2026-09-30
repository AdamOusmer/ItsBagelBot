// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface ScrollAxis {
  overflow: string;
  overscroll: string;
  at: number;
  max: number;
}

export interface ScrollBox {
  x: ScrollAxis;
  y: ScrollAxis;
}

export interface Gesture {
  deltaX: number;
  deltaY: number;
}

export interface VirtualScrollData extends Gesture {
  event: { type: string };
}

const SCROLLS = new Set(['auto', 'scroll', 'overlay']);
const GESTURE_GAP_MS = 250;

export type ScrollStyles = WeakMap<Element, ScrollStyle>;

interface ScrollStyle {
  overflowX: string;
  overflowY: string;
  overscrollBehaviorX: string;
  overscrollBehaviorY: string;
}

function along(box: ScrollBox, { deltaX, deltaY }: Gesture): { axis: ScrollAxis; delta: number } {
  return Math.abs(deltaX) >= Math.abs(deltaY)
    ? { axis: box.x, delta: deltaX }
    : { axis: box.y, delta: deltaY };
}

export function keepsWheel(box: ScrollBox, gesture: Gesture): boolean {
  const { axis, delta } = along(box, gesture);
  if (!SCROLLS.has(axis.overflow) || axis.max <= 0) return false;
  if (axis.overscroll !== 'auto') return true;
  const at = Math.round(axis.at);
  return delta > 0 ? at < axis.max : at > 0;
}

function styleOf(node: Element, cache?: ScrollStyles): ScrollStyle {
  const cached = cache?.get(node);
  if (cached) return cached;
  const { overflowX, overflowY, overscrollBehaviorX, overscrollBehaviorY } = getComputedStyle(node);
  const style = { overflowX, overflowY, overscrollBehaviorX, overscrollBehaviorY };
  cache?.set(node, style);
  return style;
}

export function readScrollBox(node: Element, cache?: ScrollStyles): ScrollBox {
  const style = styleOf(node, cache);
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

export interface NestedScrollHooks<D extends VirtualScrollData = VirtualScrollData> {
  prevent?: (node: HTMLElement) => boolean;
  virtualScroll?: (data: D) => boolean | void;
}

export interface NestedScrollGate<D extends VirtualScrollData = VirtualScrollData> {
  virtualScroll: (data: D) => boolean;
  prevent: (node: HTMLElement) => boolean;
}

export function createNestedScrollGate<D extends VirtualScrollData = VirtualScrollData>(
  outer: NestedScrollHooks<D> = {},
): NestedScrollGate<D> {
  let wheel: Gesture | null = null;
  let styles: ScrollStyles = new WeakMap();
  let lastEvent = 0;
  return {
    virtualScroll(data) {
      const now = performance.now();
      if (now - lastEvent > GESTURE_GAP_MS) styles = new WeakMap();
      lastEvent = now;
      wheel = data.event.type === 'wheel' ? data : null;
      return outer.virtualScroll?.(data) !== false;
    },
    prevent(node) {
      if (outer.prevent?.(node)) return true;
      return wheel !== null && keepsWheel(readScrollBox(node, styles), wheel);
    },
  };
}
