import type { Actions, PageServerLoad } from './$types';
import { hasGrant, accountState, setActive, publishEventSub, type AccountStatus } from '$lib/server/rpc';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

export const load: PageServerLoad = async ({ locals }) => {
  const uid = locals.session?.user_id ?? 'demo';

  let enabled = false;
  let receiving = false;
  let status: AccountStatus = 'free';

  if (env.DEMO === '1') {
    enabled = true;
    receiving = true;
    status = 'vip';
  } else {
    // Two independent reads (grant presence, plus active+tier coalesced into one
    // state_get): fire them together so SSR waits one round trip, not three.
    // allSettled keeps the page rendering even if one responder is slow or down.
    // receiving stays gated on the grant being present.
    const [grant, state] = await Promise.allSettled([hasGrant(uid), accountState(uid)]);
    enabled = grant.status === 'fulfilled' && grant.value;
    receiving = enabled && state.status === 'fulfilled' && state.value.active;
    status = state.status === 'fulfilled' ? state.value.status : 'free';
  }

  return { enabled, receiving, status };
};

export const actions: Actions = {
  // Enable: a single request to start event delivery. Marks the channel active
  // and (re)creates its EventSub subscriptions via the outgress lane.
  enable: async ({ locals }) => {
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    try {
      await setActive(uid, true);
      await publishEventSub(uid, true);
      return { ok: true, action: 'enable' };
    } catch {
      return fail(502, { error: 'enable failed' });
    }
  },
  // Restart: delete + recreate the EventSub subscriptions (stays active).
  restart: async ({ locals }) => {
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    try {
      await publishEventSub(uid, false);
      await publishEventSub(uid, true);
      return { ok: true, action: 'restart' };
    } catch {
      return fail(502, { error: 'restart failed' });
    }
  },
  // Disconnect: delete the subscriptions and mark inactive (grant kept).
  disconnect: async ({ locals }) => {
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    try {
      await publishEventSub(uid, false);
      await setActive(uid, false);
      return { ok: true, action: 'disconnect' };
    } catch {
      return fail(502, { error: 'disconnect failed' });
    }
  }
};
