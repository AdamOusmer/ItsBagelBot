// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { publicStats } from '$lib/server/public-stats';
import { publicBoards } from '$lib/server/public-boards';

export const load: PageServerLoad = async () => {
  const [stats, boards] = await Promise.all([publicStats(), publicBoards()]);
  return { stats, boards };
};
