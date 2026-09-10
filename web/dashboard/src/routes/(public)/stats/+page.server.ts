// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { publicStats } from '$lib/server/public-stats';
import { publicBoards } from '$lib/server/public-boards';

// Public page: it lives outside (app), so no auth gate runs on it (see
// routes/(app)/+layout.server.ts) and the root layout still supplies the
// resolved locale to the i18n context. Signed-out, signed-in and banned
// sessions all render the same numbers.
//
// Freshness after load belongs to the /stats/stream SSE subscription in the
// page component, not to SSR: the SSR numbers may sit up to s-maxage behind
// real time when served from the CF edge (see edgeCacheControl in
// hooks.server.ts), and hydration snaps them live within a beat.
export const load: PageServerLoad = async () => {
  const [stats, boards] = await Promise.all([publicStats(), publicBoards()]);
  return { stats, boards };
};
