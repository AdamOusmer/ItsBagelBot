// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { SongQueueDoc } from '@bagel/kit/server/songqueue-store';
import { demoSongQueueDoc } from '../../fixtures';

export * from '@bagel/kit/server/songqueue-store';

export async function getSongQueue(): Promise<SongQueueDoc> {
  return demoSongQueueDoc();
}
