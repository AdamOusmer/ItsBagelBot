// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { Layer } from './types';

export const MIN_LAYER_SIZE = 3;

/** Keeps a layer's box inside the 16:9 stage, in place. */
export function clampLayer(layer: Layer): void {
  layer.w = Math.min(100, Math.max(MIN_LAYER_SIZE, layer.w));
  layer.h = Math.min(100, Math.max(MIN_LAYER_SIZE, layer.h));
  layer.x = Math.min(100 - layer.w, Math.max(0, layer.x));
  layer.y = Math.min(100 - layer.h, Math.max(0, layer.y));
  layer.opacity = Math.min(100, Math.max(10, layer.opacity));
}
