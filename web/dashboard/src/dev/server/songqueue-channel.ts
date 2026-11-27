// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { SongQueueRead } from '#lib/server/songqueue-channel';
import { songSnapshotOf } from '#lib/songqueue-snapshot';
import { demoSongQueue } from '../fixtures';

export * from '#lib/server/songqueue-channel';

export async function readSongQueueChannel(login: string): Promise<SongQueueRead> {
  return { login, channelName: login, snapshot: songSnapshotOf(demoSongQueue(), Date.now()), outcome: 'page' };
}
