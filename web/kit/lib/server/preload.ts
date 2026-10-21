// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const FIRST_PAINT_FONT = /\/(syne-latin|dm-sans-latin|dm-mono-400)\.[\w-]+\.woff2$/;

export function preloadStrategy(input: { type: string; path: string }): boolean {
  if (input.type === 'font') return FIRST_PAINT_FONT.test(input.path);
  return input.type === 'js' || input.type === 'css';
}
