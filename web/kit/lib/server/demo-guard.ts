// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const EXPLICIT_OFF = ['0', 'false', 'off', 'no'];

export function demoConfigured(env: Record<string, string | undefined>): boolean {
  return Object.entries(env).some(
    ([key, value]) => key === 'DEMO' && !!value && !EXPLICIT_OFF.includes(value.toLowerCase())
  );
}
