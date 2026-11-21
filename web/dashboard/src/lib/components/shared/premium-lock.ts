// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function premiumLocked(data: Record<string, unknown>): boolean {
  return data.isPremium !== true;
}

export function premiumSwitchLocked(premium: boolean | undefined, active: boolean, data: Record<string, unknown>): boolean {
  if (premium !== true || active) return false;
  return premiumLocked(data);
}
