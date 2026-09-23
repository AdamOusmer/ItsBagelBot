// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { error, json } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { requireRole } from '$lib/server/access';
import { trialList } from '$lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

export const GET: RequestHandler = async ({ locals }) => {
  if (!(await requireRole({ locals }, 'trials.manage'))) throw error(403, 'forbidden');
  if (DEMO) return json({ snapshot: { version: 1, trials: [] } });
  try {
    return json({ snapshot: await trialList() });
  } catch {
    throw error(503, 'Trial state is unavailable.');
  }
};
