// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { hasFinePointer, prefersReducedMotion } from './motion-query';
import { subscribe, wake, type Tick } from './raf-loop';

export type MagneticOptions = { strength?: number; max?: number };

const EASE = 0.2;

const SETTLE_PX = 0.1;

const NOOP = () => {};

export function mountMagnetic(node: HTMLElement, opts?: MagneticOptions): () => void {
  if (!hasFinePointer() || prefersReducedMotion()) return NOOP;

  const strength = opts?.strength ?? 0.3;
  const max = opts?.max ?? 14;
  let tx = 0,
    ty = 0,
    cx = 0,
    cy = 0;

  node.style.transition = 'transform 0.3s cubic-bezier(0.16,1,0.3,1)';
  node.style.willChange = 'transform';

  const cap = (v: number) => Math.max(-max, Math.min(max, v));

  const tick: Tick = () => {
    cx += (tx - cx) * EASE;
    cy += (ty - cy) * EASE;
    node.style.transform = `translate(${cx.toFixed(2)}px, ${cy.toFixed(2)}px)`;
    return Math.abs(tx - cx) > SETTLE_PX || Math.abs(ty - cy) > SETTLE_PX;
  };
  const unsubscribe = subscribe(tick);

  const move = (e: PointerEvent) => {
    const r = node.getBoundingClientRect();
    tx = cap((e.clientX - (r.left + r.width / 2)) * strength);
    ty = cap((e.clientY - (r.top + r.height / 2)) * strength);
    node.style.transition = 'none';
    wake(tick);
  };
  const leave = () => {
    tx = 0;
    ty = 0;
    node.style.transition = 'transform 0.4s cubic-bezier(0.16,1,0.3,1)';
    wake(tick);
  };

  node.addEventListener('pointermove', move);
  node.addEventListener('pointerleave', leave);

  return () => {
    unsubscribe();
    node.removeEventListener('pointermove', move);
    node.removeEventListener('pointerleave', leave);
  };
}

export function observeMagnetic(root: ParentNode = document): () => void {
  const disposers: (() => void)[] = [];
  for (const el of root.querySelectorAll<HTMLElement>('[data-magnetic]')) {
    const [s, m] = (el.dataset.magnetic ?? '').split(',');
    const strength = Number.parseFloat(s);
    const max = Number.parseFloat(m);
    disposers.push(
      mountMagnetic(el, {
        strength: Number.isFinite(strength) ? strength : undefined,
        max: Number.isFinite(max) ? max : undefined,
      }),
    );
  }
  return () => {
    for (const dispose of disposers) dispose();
  };
}
