// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error, json } from '@sveltejs/kit';
import { requireAdmin } from '#lib/server/access';
import { directoryQuery, loadUserDirectory } from '../+page.server';

// Refresh target for the users page while the live filter is on. The page
// load is a snapshot; this is the same query read again so the list tracks
// who goes on and off air while it stays open.
export async function GET({ locals, url }) {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw error(403, 'forbidden');
  const q = directoryQuery(url);
  if (!q.live) throw error(400, 'live filter required');
  return json(await loadUserDirectory(admin.id, q));
}
