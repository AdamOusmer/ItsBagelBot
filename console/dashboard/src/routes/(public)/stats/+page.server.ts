// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { publicStats } from '$lib/server/public-stats';

// Public page: it lives outside (app), so no auth gate runs on it (see
// routes/(app)/+layout.server.ts) and the root layout still supplies the
// resolved locale to the i18n context. Signed-out, signed-in and banned
// sessions all render the same numbers.
//
// no-store because the payload is a live counter snapshot: a CDN or browser
// cache holding it would show a frozen "live" page.
export const load: PageServerLoad = async ({ setHeaders }) => {
  setHeaders({ 'cache-control': 'no-store' });
  return { stats: await publicStats() };
};
