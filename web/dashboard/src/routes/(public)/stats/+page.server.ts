// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { publicStats } from '#lib/server/public-stats';
import { publicBoards } from '#lib/server/public-boards';
import { markEdge } from '#lib/server/edge-cache';

export const load: PageServerLoad = async ({ locals }) => {
  const [stats, boards] = await Promise.all([publicStats(), publicBoards()]);
  markEdge(locals, 'page');
  return { stats, boards };
};
