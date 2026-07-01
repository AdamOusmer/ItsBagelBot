import type { Actions, PageServerLoad } from './$types';
import type { CommandView } from '@bagel/shared';
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
} from '$lib/server/services';
import { listCommands } from '$lib/server/commands-store';
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

// Parse the human-formatted uses counter ('1.2k', '412') for ranking.
function usesCount(raw: string | undefined): number {
  if (!raw) return 0;
  const m = raw.trim().toLowerCase().match(/^([\d.]+)(k|m)?$/);
  if (!m) return 0;
  const n = Number(m[1]) || 0;
  return m[2] === 'm' ? n * 1_000_000 : m[2] === 'k' ? n * 1000 : n;
}

const demoTop: CommandView[] = [
  { name: 'bagel', response: '{user} tosses a warm bagel to {target}. Toasty.', is_active: true, uses: '1.2k' },
  { name: 'lurk', response: '{user} fades into the shadows. Thanks for the lurk.', is_active: true, uses: '521' },
  { name: 'uptime', response: '@{user} the stream has been live for {uptime} 🥯', is_active: true, uses: '412' }
];

export const load: PageServerLoad = ({ locals }) => {
  const uid = locals.session?.user_id ?? 'demo';

  // Return the RPCs as unawaited promises so SvelteKit streams them: the page
  // shell flushes immediately and each section hydrates when its round trip
  // lands, instead of blocking SSR (and the post-login redirect) on NATS.
  const conn: Promise<ConnState> =
    env.DEMO === '1'
      ? Promise.resolve({ enabled: true, receiving: true, status: 'vip', subState: 'ok', subError: '' })
      : connState(uid);

  // Most-used active commands for the home strip. Cache-backed (same fabric
  // entry as the commands page) and optional: a failure just hides the strip.
  const top: Promise<CommandView[]> =
    env.DEMO === '1'
      ? Promise.resolve(demoTop)
      : listCommands(uid)
          .then((cmds) =>
            cmds
              .filter((c) => c.is_active)
              .toSorted((a, b) => usesCount(b.uses) - usesCount(a.uses))
              .slice(0, 3)
          )
          .catch(() => [] as CommandView[]);

  return { conn, top };
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
