// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { finePointer, reduceMotion } from './motion-query';
import { subscribe, wake, type Tick } from './raf-loop';
import { cachedMeasure } from './rect-cache';

export type TiltOptions = { max?: number };

const DEFAULT_MAX_DEG = 4;

const NOOP = () => {};

function setTilt(el: HTMLElement, x: string, y: string): void {
  el.style.setProperty('--tilt-x', `${x}deg`);
  el.style.setProperty('--tilt-y', `${y}deg`);
}

function maxOf(el: HTMLElement, options: TiltOptions): number {
  return options.max ?? (Number.parseFloat(el.dataset.tilt ?? '') || DEFAULT_MAX_DEG);
}

export function mountTilt(el: HTMLElement, options: TiltOptions = {}): () => void {
  if (el.dataset.tiltReady === 'true') return NOOP;
  el.dataset.tiltReady = 'true';

  const max = maxOf(el, options);
  const bounds = cachedMeasure(() => el.getBoundingClientRect());
  let pending: { x: number; y: number } | null = null;
  let active = false;

  const tick: Tick = () => {
    if (pending) {
      const rect = bounds.read();
      const px = (pending.x - rect.left) / rect.width - 0.5;
      const py = (pending.y - rect.top) / rect.height - 0.5;
      setTilt(el, (-py * max).toFixed(2), (px * max).toFixed(2));
      pending = null;
    }
    return false;
  };
  const unsubscribe = subscribe(tick);

  const enter = () => {
    if (active) return;
    active = true;
    bounds.watch();
  };
  const move = (event: PointerEvent) => {
    if (!finePointer.matches || reduceMotion.matches) return;
    enter();
    pending = { x: event.clientX, y: event.clientY };
    wake(tick);
  };
  const leave = () => {
    active = false;
    pending = null;
    bounds.unwatch();
    setTilt(el, '0', '0');
  };

  el.addEventListener('pointerenter', enter);
  el.addEventListener('pointermove', move);
  el.addEventListener('pointerleave', leave);
  setTilt(el, '0', '0');

  return () => {
    unsubscribe();
    if (active) bounds.unwatch();
    el.removeEventListener('pointerenter', enter);
    el.removeEventListener('pointermove', move);
    el.removeEventListener('pointerleave', leave);
    delete el.dataset.tiltReady;
  };
}

export function observeTilt(root: ParentNode = document): () => void {
  const disposers = Array.from(root.querySelectorAll<HTMLElement>('[data-tilt]'), (el) => mountTilt(el));
  return () => {
    for (const dispose of disposers) dispose();
  };
}
