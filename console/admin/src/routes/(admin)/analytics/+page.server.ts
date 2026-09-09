// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';

// The enrollment series folded into the Overview; keep old bookmarks working.
export const load: PageServerLoad = () => {
  throw redirect(301, '/');
};
