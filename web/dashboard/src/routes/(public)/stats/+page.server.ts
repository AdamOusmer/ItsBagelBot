// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { publicStats } from '#lib/server/public-stats';
import { publicBoards } from '#lib/server/public-boards';
import { markEdge, pageOutcome } from '#lib/server/edge-cache';

export const load: PageServerLoad = async ({ locals }) => {
  const [stats, boards] = await Promise.all([publicStats(), publicBoards()]);
  const rates = [stats.msg_rate, stats.event_rate, stats.msg_rate_now, stats.event_rate_now];
  markEdge(locals, pageOutcome(rates.map((rate) => rate === null ? null : true), stats.degraded));
  return { stats, boards };
};
