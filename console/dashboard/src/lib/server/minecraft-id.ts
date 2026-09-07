// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const UNDASHED = /^[0-9a-f]{32}$/i;
const DASHED = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// canonicalMinecraftUUID returns the lowercase undashed form when value is
// already a uuid (dashes optional), otherwise null.
export function canonicalMinecraftUUID(value: string): string | null {
  const s = value.trim();
  if (!s) return null;
  const hex = DASHED.test(s) ? s.replace(/-/g, '') : s;
  if (!UNDASHED.test(hex)) return null;
  return hex.toLowerCase();
}
