// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { listCounters } from '$lib/server/loyalty-store';
import type { RequestHandler } from './$types';

const DEMO = dev && env.DEMO === '1';

function delegateBlocked(s: App.Locals['session']): boolean {
  if (!s?.delegate_of) return false;
  const sections = s.sections ?? [];
  return !sections.includes('commands') && !sections.includes('modules');
}

export const GET: RequestHandler = async ({ locals }) => {
  const s = locals.session;
  if (delegateBlocked(s)) return json({ counters: [] }, { status: 403 });
  const uid = s?.delegate_of ?? s?.user_id;
  if (DEMO) return json({ counters: [] });
  if (!uid) return json({ counters: [] }, { status: 401 });
  try {
    const counters = (await listCounters(uid)).map(({ name, scope }) => ({ name, scope }));
    return json({ counters });
  } catch {
    return json({ counters: [] }, { status: 503 });
  }
};
