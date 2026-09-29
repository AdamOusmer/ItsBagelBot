// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { finePointer, reduceMotion } from './motion-query';

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
  const reset = () => setTilt(el, '0', '0');
  const move = (event: PointerEvent) => {
    if (!finePointer.matches || reduceMotion.matches) return;
    const rect = el.getBoundingClientRect();
    const px = (event.clientX - rect.left) / rect.width - 0.5;
    const py = (event.clientY - rect.top) / rect.height - 0.5;
    setTilt(el, (-py * max).toFixed(2), (px * max).toFixed(2));
  };

  el.addEventListener('pointermove', move);
  el.addEventListener('pointerleave', reset);
  reset();

  return () => {
    el.removeEventListener('pointermove', move);
    el.removeEventListener('pointerleave', reset);
    delete el.dataset.tiltReady;
  };
}

export function observeTilt(root: ParentNode = document): () => void {
  const disposers = Array.from(root.querySelectorAll<HTMLElement>('[data-tilt]'), (el) => mountTilt(el));
  return () => {
    for (const dispose of disposers) dispose();
  };
}
