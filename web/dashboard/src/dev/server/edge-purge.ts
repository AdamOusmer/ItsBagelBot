// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export * from '#lib/server/edge-purge';

export async function purgeEdgeByTag(): Promise<boolean> {
  return true;
}

export async function purgeChannelPages(): Promise<boolean> {
  return true;
}

export function schedulePurgeChannel(): void {}

export function scheduleLeaderboardPurge(): void {}

export function purgeRenamedChannel(): void {}
