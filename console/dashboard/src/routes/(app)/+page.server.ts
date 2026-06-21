import type { Actions, PageServerLoad } from './$types';
import {
  hasGrant,
  accountState,
  setActive,
  publishEventSub,
  publishEventSubReconnect,
  channelSubState,
  auditDashboardImpersonation,
  type AccountStatus,
  type ChannelSubState
} from '$lib/server/rpc';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

export type ConnState = {
  enabled: boolean;
  receiving: boolean;
  status: AccountStatus;
  subState: ChannelSubState['state'];
  subError: string;
};

// Resolve the bot connection state in one round trip (grant presence + the
// coalesced active/tier state_get + channel enroll state). allSettled keeps a
// slow or down responder from failing the whole render.
async function connState(uid: string): Promise<ConnState> {
  const [grant, state, sub] = await Promise.allSettled([
    hasGrant(uid),
    accountState(uid),
    channelSubState(uid)
  ]);
  const enabled = grant.status === 'fulfilled' && grant.value;
  const receiving = enabled && state.status === 'fulfilled' && state.value.active;
  const status: AccountStatus = state.status === 'fulfilled' ? state.value.status : 'free';
  const subState: ChannelSubState['state'] = sub.status === 'fulfilled' ? sub.value.state : 'unknown';
  const subError: string = sub.status === 'fulfilled' ? sub.value.error : '';
  return { enabled, receiving, status, subState, subError };
}

export const load: PageServerLoad = ({ locals }) => {
  const uid = locals.session?.user_id ?? 'demo';

  // Return the RPC as an unawaited promise so SvelteKit streams it: the page
  // shell flushes immediately and the connection state hydrates when the round
  // trip lands, instead of blocking SSR (and the post-login redirect) on NATS.
  const conn: Promise<ConnState> =
    env.DEMO === '1'
      ? Promise.resolve({ enabled: true, receiving: true, status: 'vip', subState: 'ok', subError: '' })
      : connState(uid);

  return { conn };
};

export const actions: Actions = {
  // Enable: mark the channel active and atomically (re)create EventSub subs.
  enable: async ({ locals }) => {
    if (locals.session?.delegate_of) return fail(403);
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    try {
      await setActive(uid, true);
      await publishEventSubReconnect(uid);
      auditDashboardImpersonation(locals.session, 'enable');
      return { ok: true, action: 'enable' };
    } catch {
      return fail(502, { error: 'enable failed' });
    }
  },
  // Restart: atomic drop + recreate of EventSub subscriptions (stays active).
  restart: async ({ locals }) => {
    if (locals.session?.delegate_of) return fail(403);
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    try {
      await publishEventSubReconnect(uid);
      auditDashboardImpersonation(locals.session, 'restart');
      return { ok: true, action: 'restart' };
    } catch {
      return fail(502, { error: 'restart failed' });
    }
  },
  // Disconnect: delete the subscriptions and mark inactive (grant kept).
  disconnect: async ({ locals }) => {
    if (locals.session?.delegate_of) return fail(403);
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    try {
      await publishEventSub(uid, false);
      await setActive(uid, false);
      auditDashboardImpersonation(locals.session, 'disconnect');
      return { ok: true, action: 'disconnect' };
    } catch {
      return fail(502, { error: 'disconnect failed' });
    }
  }
};
