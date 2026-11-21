// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { requireRole } from '#lib/server/access';
import { giveawayHistory } from '#lib/server/giveaways';

const NUMERIC_USER_ID = /^[0-9]+$/;

export const GET: RequestHandler = async ({ locals, url }) => {
  const admin = await requireRole({ locals }, 'giveaways.manage');
  if (!admin) return json({ error: 'forbidden' }, { status: 403 });

  const userId = url.searchParams.get('q')?.trim() ?? '';
  if (!NUMERIC_USER_ID.test(userId)) return json({ error: 'numeric user id required' }, { status: 400 });

  try {
    const awards = await giveawayHistory({ actorId: admin.id, userId });
    return json({ awards });
  } catch (error) {
    const message = error instanceof Error ? error.message : 'history unavailable';
    return json({ error: message }, { status: 502 });
  }
};
