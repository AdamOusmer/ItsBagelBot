// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { channelSubState, accountState } from '$lib/server/services';
import type { RequestHandler } from './$types';

const DEMO = dev && env.DEMO === '1';

function fulfilled<T>(result:PromiseSettledResult<T>): T | undefined {
 return result.status === 'fulfilled' ? result.value : undefined;
}

export const GET: RequestHandler = async ({ locals }) => {
  if (locals.session?.delegate_of) return json({ state: 'unknown', error: 'forbidden' }, { status: 403 });
  const uid = locals.session?.user_id;
  if (!uid) return json({ state: 'unknown', error: '' }, { status: 401 });
  if (DEMO) return json({ state: 'ok', error: '' });
  const [sub, account] = await Promise.allSettled([channelSubState(uid), accountState(uid)]);
  return json(connectionSnapshot(fulfilled(sub),fulfilled(account)));
};

function connectionSnapshot(sub:Awaited<ReturnType<typeof channelSubState>>|undefined,account:Awaited<ReturnType<typeof accountState>>|undefined) {
 return {...(sub ?? {state:'unknown',error:''}),lifecycle:account?.lifecycle,lifecycleRevision:account?.lifecycleRevision,accountCreatedAt:account?.accountCreatedAt};
}
