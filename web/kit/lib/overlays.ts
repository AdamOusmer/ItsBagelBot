// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const MIN_EVERY_MINUTES = 5;
export const MAX_EVERY_MINUTES = 120;
export const DEFAULT_EVERY_MINUTES = 15;
export const MIN_SHOW_SECONDS = 6;
export const MAX_SHOW_SECONDS = 20;
export const DEFAULT_SHOW_SECONDS = 8;

export const POWERED_DIRECTIONS = ['top', 'bottom', 'left', 'right'] as const;
export const POWERED_ALIGNS = ['start', 'center', 'end'] as const;
export const DEFAULT_POWERED_ALIGN: PoweredAlign = 'end';

export const POWERED_PAGE_URL = 'https://overlay.itsbagelbot.com/powered';

export type PoweredDirection = (typeof POWERED_DIRECTIONS)[number];
export type PoweredAlign = (typeof POWERED_ALIGNS)[number];

export interface PoweredConfig {
  every_minutes: number;
  show_seconds: number;
  directions: PoweredDirection[];
  align: PoweredAlign;
}

export type PoweredConfigError = 'overlays.invalidEvery' | 'overlays.invalidShow' | 'overlays.invalidFrom' | 'overlays.invalidAlign';

export function defaultPoweredConfig(): PoweredConfig {
  return {
    every_minutes: DEFAULT_EVERY_MINUTES,
    show_seconds: DEFAULT_SHOW_SECONDS,
    directions: [...POWERED_DIRECTIONS],
    align: DEFAULT_POWERED_ALIGN
  };
}

const inRange = (value: number, min: number, max: number) => Number.isInteger(value) && Math.min(Math.max(value, min), max) === value;

function validDirections(list: readonly string[]): boolean {
  const known = list.every((item) => (POWERED_DIRECTIONS as readonly string[]).includes(item));
  const unique = list.length > 0 && new Set(list).size === list.length;
  return known && unique;
}

export function poweredConfigError(config: PoweredConfig): PoweredConfigError | null {
  if (!inRange(config.every_minutes, MIN_EVERY_MINUTES, MAX_EVERY_MINUTES)) return 'overlays.invalidEvery';
  if (!inRange(config.show_seconds, MIN_SHOW_SECONDS, MAX_SHOW_SECONDS)) return 'overlays.invalidShow';
  if (!validDirections(config.directions)) return 'overlays.invalidFrom';
  if (!(POWERED_ALIGNS as readonly string[]).includes(config.align)) return 'overlays.invalidAlign';
  return null;
}

export function poweredPreviewUrl(config: PoweredConfig, premium: boolean): string {
  const query = new URLSearchParams({
    preview: '1',
    show: String(config.show_seconds),
    from: config.directions.join(','),
    align: config.align,
    premium: premium ? '1' : '0'
  });
  return `${POWERED_PAGE_URL}?${query}`;
}
