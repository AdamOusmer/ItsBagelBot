// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AnalyticsReads } from '#lib/analytics-page';
import { demoAnalytics } from '../fixtures';

export * from '#lib/server/analytics-reads';

export async function readAnalytics(_actorId: string, days: number): Promise<AnalyticsReads> {
  return demoAnalytics(days);
}
