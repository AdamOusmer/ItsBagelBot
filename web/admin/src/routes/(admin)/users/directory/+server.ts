// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error, json } from '@sveltejs/kit';
import { requireAdmin } from '#lib/server/access';
import { directoryQuery, loadUserDirectory } from '#lib/server/user-directory';
import { liveUserCount } from '#lib/server/live-count';

export async function GET({ locals, url }) {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw error(403, 'forbidden');
  const q = directoryQuery(url);
  const [directory, liveCount] = await Promise.all([
    q.live ? loadUserDirectory(admin.id, q) : null,
    liveUserCount(admin.id)
  ]);
  return json({ directory, liveCount });
}
