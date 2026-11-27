// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { QueueRead } from '#lib/server/queue-channel';
import { snapshotOf } from '#lib/queue-snapshot';
import { demoQueue } from '../fixtures';

export * from '#lib/server/queue-channel';

export async function readQueueChannel(login: string): Promise<QueueRead> {
  return { login, channelName: login, snapshot: snapshotOf(demoQueue(), Date.now()), outcome: 'page' };
}
