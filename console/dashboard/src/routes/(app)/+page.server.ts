import type { Actions, PageServerLoad } from './$types';
import { hasGrant, isActive, setActive, publishEventSub } from '$lib/server/rpc';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

export const load: PageServerLoad = async ({ locals }) => {
  const uid = locals.session?.user_id ?? 'demo';

  let enabled = false;
  let receiving = false;
  if (env.DEMO === '1') {
    enabled = true;
    receiving = true;
  } else {
    try {
      enabled = await hasGrant(uid);
      receiving = enabled && (await isActive(uid));
    } catch {
      /* degrade: render with defaults */
    }
  }

  return { enabled, receiving };
};

export const actions: Actions = {
  // Restart: tear down the channel's EventSub subscriptions and recreate them,
  // both routed through outgress (shared Helix rate-limit bucket). Stays active.
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
  // Disconnect: delete the EventSub subscriptions and mark the channel inactive.
  // The stored grant is kept, so reconnecting needs no new consent.
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
