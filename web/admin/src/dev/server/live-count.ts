// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { sampleLiveUserIDs, sampleUsers } from '../fixtures';

export * from '#lib/server/live-count';

export async function liveUserCount(): Promise<number> {
  return sampleUsers.filter((user) => sampleLiveUserIDs.has(user.id)).length;
}
