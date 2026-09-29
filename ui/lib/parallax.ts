// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { hasFinePointer, prefersReducedMotion } from './motion-query';
import { subscribe, wake, type Tick } from './raf-loop';

export type ParallaxPoint = { px: number; py: number };

export type ParallaxScope = 'element' | 'viewport';

export type ParallaxOptions = {
  onmove: (point: ParallaxPoint) => void;
  scope?: ParallaxScope;
};

type Frame = { left: number; top: number; width: number; height: number };

const CENTRE: ParallaxPoint = { px: 0, py: 0 };

function normalise(offset: number, size: number): number {
  return size > 0 ? (offset / size - 0.5) * 2 : 0;
}

function frameOf(node: HTMLElement, scope: ParallaxScope): Frame {
  if (scope === 'viewport') return { left: 0, top: 0, width: window.innerWidth, height: window.innerHeight };
  return node.getBoundingClientRect();
}

export function mountParallax(node: HTMLElement, options: ParallaxOptions): () => void {
  const scope = options.scope ?? 'element';
  const source: EventTarget = scope === 'viewport' ? window : node;
  let pending: { x: number; y: number } | null = null;

  const tick: Tick = () => {
    if (!pending) return false;
    const frame = frameOf(node, scope);
    options.onmove({
      px: normalise(pending.x - frame.left, frame.width),
      py: normalise(pending.y - frame.top, frame.height),
    });
    pending = null;
    return false;
  };
  const unsubscribe = subscribe(tick);

  const move = (event: Event) => {
    if (!hasFinePointer() || prefersReducedMotion()) return;
    const { clientX, clientY } = event as PointerEvent;
    pending = { x: clientX, y: clientY };
    wake(tick);
  };
  const leave = () => {
    pending = null;
    options.onmove(CENTRE);
  };

  source.addEventListener('pointermove', move);
  if (scope === 'element') node.addEventListener('pointerleave', leave);

  return () => {
    unsubscribe();
    source.removeEventListener('pointermove', move);
    node.removeEventListener('pointerleave', leave);
  };
}
