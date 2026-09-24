// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const UNITS = ['ns', 'µs', 'ms', 's'] as const;

export function durationLabel(ns: number): string {
  let value = Math.max(0, ns);
  let unit = 0;
  while (value >= 1000 && unit < UNITS.length - 1) {
    value /= 1000;
    unit++;
  }
  return `${value.toFixed(2)} ${UNITS[unit]}`;
}
