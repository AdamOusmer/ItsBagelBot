// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import type { ShardSnapshot } from '@bagel/kit';
import { shardSnapshot } from '#lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

export function readShardSnapshot(): Promise<ShardSnapshot> {
  return DEMO
    ? import('#lib/server/demo-data').then(({ sampleSnapshot }) => sampleSnapshot)
    : shardSnapshot();
}
