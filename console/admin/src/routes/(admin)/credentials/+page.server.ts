// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';

// The credentials page became the Secrets console; keep old bookmarks working.
export const load: PageServerLoad = () => {
  throw redirect(301, '/secrets');
};
