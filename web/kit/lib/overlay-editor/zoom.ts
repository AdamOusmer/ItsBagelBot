// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const ZOOM_STEPS = [0.25, 0.5, 0.75, 1, 1.5, 2] as const;

const EPSILON = 0.005;

export function stepZoom(scale: number, direction: -1 | 1): number {
  if (direction > 0) return ZOOM_STEPS.find((step) => step > scale + EPSILON) ?? ZOOM_STEPS[ZOOM_STEPS.length - 1];
  return ZOOM_STEPS.findLast((step) => step < scale - EPSILON) ?? ZOOM_STEPS[0];
}
