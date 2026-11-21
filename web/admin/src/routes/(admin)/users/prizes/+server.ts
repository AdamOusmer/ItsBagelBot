// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { requireRole } from '#lib/server/access';
import { giveawayHistory } from '#lib/server/giveaways';

export const GET: RequestHandler = async ({ locals, url }) => {
  const admin = await requireRole({ locals }, 'giveaways.manage');
  if (!admin) return json({ error: 'forbidden' }, { status: 403 });
  const userId = String(url.searchParams.get('q') ?? '').trim();
  if (!/^[0-9]+$/.test(userId)) return json({ error: 'numeric user id required' }, { status: 400 });
  try {
    return json({ awards: await giveawayHistory({ actorId: admin.id, userId }) });
  } catch (error) {
    return json({ error: error instanceof Error ? error.message : 'history unavailable' }, { status: 502 });
  }
};
