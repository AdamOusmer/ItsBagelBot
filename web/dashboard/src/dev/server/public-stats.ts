// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PublicStats } from '#lib/server/public-stats';
import { demoStats } from '../fixtures';

export * from '#lib/server/public-stats';

export async function publicStats(): Promise<PublicStats> {
  return demoStats();
}
