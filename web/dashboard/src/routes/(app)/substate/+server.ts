// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { channelSubState } from '$lib/server/services';
import type { RequestHandler } from './$types';

const DEMO = dev && env.DEMO === '1';

export const GET: RequestHandler = async ({ locals }) => {
  if (locals.session?.delegate_of) return json({ state: 'unknown', error: 'forbidden' }, { status: 403 });
  const uid = locals.session?.user_id;
  if (!uid) return json({ state: 'unknown', error: '' }, { status: 401 });
  if (DEMO) return json({ state: 'ok', error: '' });
  return json(await channelSubState(uid));
};
