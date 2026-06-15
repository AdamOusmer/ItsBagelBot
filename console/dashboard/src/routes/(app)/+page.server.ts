import type { Actions, PageServerLoad } from './$types';
import { hasGrant, isActive, setActive, tier, publishEventSub } from '$lib/server/rpc';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';
import type { Tier } from '@bagel/shared';

export const load: PageServerLoad = async ({ locals }) => {
  const uid = locals.session?.user_id ?? 'demo';

  let enabled = false;
  let receiving = false;
  let accountTier: Tier = 'standard';

  if (env.DEMO === '1') {
    enabled = true;
    receiving = true;
    accountTier = 'premium';
  } else {
    try {
      enabled = await hasGrant(uid);
      receiving = enabled && (await isActive(uid));
    } catch {
      /* degrade */
    }
    try {
      accountTier = await tier(uid);
    } catch {
      /* keep standard */
    }
  }

  return { enabled, receiving, tier: accountTier };
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
