// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { hasFinePointer, prefersReducedMotion } from './motion-query';
import { subscribe, wake, type Tick } from './raf-loop';
import { cachedMeasure } from './rect-cache';

export type MagneticOptions = { strength?: number; max?: number };

const EASE = 0.2;

const SETTLE_PX = 0.1;

const NOOP = () => {};

function renderedOffset(node: HTMLElement): { x: number; y: number } {
  const transform = getComputedStyle(node).transform;
  if (!transform || transform === 'none') return { x: 0, y: 0 };
  const matrix = new DOMMatrixReadOnly(transform);
  return { x: matrix.m41, y: matrix.m42 };
}

function restCenter(node: HTMLElement): { x: number; y: number } {
  const rect = node.getBoundingClientRect();
  const offset = renderedOffset(node);
  return { x: rect.left + rect.width / 2 - offset.x, y: rect.top + rect.height / 2 - offset.y };
}

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
  const home = cachedMeasure(() => restCenter(node));
  let active = false;

  const tick: Tick = () => {
    cx += (tx - cx) * EASE;
    cy += (ty - cy) * EASE;
    node.style.transform = `translate(${cx.toFixed(2)}px, ${cy.toFixed(2)}px)`;
    return Math.abs(tx - cx) > SETTLE_PX || Math.abs(ty - cy) > SETTLE_PX;
  };
  const unsubscribe = subscribe(tick);

  const enter = () => {
    if (active) return;
    active = true;
    home.watch();
    node.style.transition = 'none';
  };
  const move = (e: PointerEvent) => {
    enter();
    const c = home.read();
    tx = cap((e.clientX - (c.x + cx)) * strength);
    ty = cap((e.clientY - (c.y + cy)) * strength);
    wake(tick);
  };
  const leave = () => {
    active = false;
    home.unwatch();
    tx = 0;
    ty = 0;
    node.style.transition = 'transform 0.4s cubic-bezier(0.16,1,0.3,1)';
    wake(tick);
  };

  node.addEventListener('pointerenter', enter);
  node.addEventListener('pointermove', move);
  node.addEventListener('pointerleave', leave);

  return () => {
    unsubscribe();
    if (active) home.unwatch();
    node.removeEventListener('pointerenter', enter);
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
