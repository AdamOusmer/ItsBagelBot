import { json } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import { channelSubState } from '$lib/server/services';
import type { RequestHandler } from './$types';

// Lightweight poll target: the dashboard fires a reconnect, then polls this
// until outgress flips the channel's enroll state off "pending" (to "ok" or
// "failing"). Kept tiny because the page hits it on a short interval.
export const GET: RequestHandler = async ({ locals }) => {
  const uid = locals.session?.user_id;
  if (!uid) return json({ state: 'unknown', error: '' }, { status: 401 });
  if (env.DEMO === '1') return json({ state: 'ok', error: '' });
  return json(await channelSubState(uid));
};
