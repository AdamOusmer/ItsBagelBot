// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { error, json } from '@sveltejs/kit';
import { requireRole } from '#lib/server/access';
import { readTrialSnapshot } from '../trial-snapshot.server';

export const GET: RequestHandler = async ({ locals }) => {
  if (!(await requireRole({ locals }, 'trials.manage'))) throw error(403, 'forbidden');
  try {
    return json({ snapshot: await readTrialSnapshot() }, { headers: { 'cache-control': 'no-store' } });
  } catch {
    throw error(503, 'Trial state is unavailable.');
  }
};
