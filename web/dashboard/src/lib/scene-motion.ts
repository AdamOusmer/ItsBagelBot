// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fly } from 'svelte/transition';
import { bezier } from '@bagel/ui/lib/tween';
import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';

const expo = bezier(0.16, 1, 0.3, 1);

export const TRAVEL_IN = 72;
export const TRAVEL_OUT = 56;

export function lateralFly(node: Element, params: { x: number; duration: number; delay?: number }) {
  if (prefersReducedMotion()) return { duration: 0 };
  return fly(node, { ...params, easing: expo, opacity: 0 });
}
