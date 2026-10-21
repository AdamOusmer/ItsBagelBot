// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function leaderboardCacheTag(login: string): string {
  return `lb:${login}`;
}
