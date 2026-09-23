// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Old onboarding import links resume inside the welcome journey.
import { redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ url }) => {
  const next = new URL('/welcome', url);
  next.searchParams.set('import', '1');
  for (const key of ['source', 'e']) {
    const value = url.searchParams.get(key);
    if (value) next.searchParams.set(key, value);
  }
  throw redirect(303, `${next.pathname}${next.search}`);
};
