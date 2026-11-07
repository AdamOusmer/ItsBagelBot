// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type Resolution = '720p' | '1080p' | '2k';

export const RESOLUTIONS: Record<Resolution, { width: number; height: number; label: string }> = {
  '720p': { width: 1280, height: 720, label: '720p' },
  '1080p': { width: 1920, height: 1080, label: '1080p' },
  '2k': { width: 2560, height: 1440, label: '2K' },
};

export const RESOLUTION_OPTIONS: readonly Resolution[] = ['720p', '1080p', '2k'];

/** A layer's x/y/w/h and font size are stored as a percent of the canvas, so
    switching resolution (always 16:9) never moves or rescales anything; these
    convert that percent to and from canvas pixels for display only. */
export function pxFromPercent(percent: number, span: number): number {
  return Math.round((percent / 100) * span);
}

export function percentFromPx(px: number, span: number): number {
  return span > 0 ? (px / span) * 100 : 0;
}
