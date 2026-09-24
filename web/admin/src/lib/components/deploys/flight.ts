// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fly } from 'svelte/transition';
import { bezier } from '@bagel/ui/lib/tween';
import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';

const expo = bezier(0.16, 1, 0.3, 1);
const sideOf = (i: number) => (i % 2 === 0 ? 1 : -1);

export type Beat = { i?: number };

export function flight(dir: () => number) {
  return {
    arrive: (node: Element, { i = 0 }: Beat = {}) =>
      prefersReducedMotion()
        ? { duration: 0 }
        : fly(node, { x: dir() * 72 * sideOf(i), duration: 760, delay: 140 + i * 70, easing: expo, opacity: 0 }),
    depart: (node: Element, { i = 0 }: Beat = {}) =>
      prefersReducedMotion()
        ? { duration: 0 }
        : fly(node, { x: -dir() * 56 * sideOf(i), duration: 380, delay: i * 35, easing: expo, opacity: 0 })
  };
}
