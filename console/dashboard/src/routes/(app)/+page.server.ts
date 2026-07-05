import type { Actions, PageServerLoad } from './$types';
import type { CommandView } from '@bagel/shared';
import { MODULE_CATALOG } from '@bagel/shared';
import {
  hasGrant,
  accountState,
  setActive,
  setOnboarded,
  publishEventSub,
  publishEventSubReconnect,
  channelSubState,
  auditDashboardImpersonation,
  delegationList,
  type AccountStatus,
  type ChannelSubState
} from '$lib/server/services';
import { listCommands, listModules } from '$lib/server/commands-store';
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

// Parse the uses counter for ranking: the backend sends a plain number, while
// older sample data used human-formatted strings ('1.2k', '412').
function usesCount(raw: number | string | undefined): number {
  if (typeof raw === 'number') return raw;
  if (!raw) return 0;
  const m = raw.trim().toLowerCase().match(/^([\d.]+)(k|m)?$/);
  if (!m) return 0;
  const n = Number(m[1]) || 0;
  return m[2] === 'm' ? n * 1_000_000 : m[2] === 'k' ? n * 1000 : n;
}

// Everything the home page shows about commands, from one cached read: the
// most-used rows for the strip plus real counts for the stat cards.
export type CommandDigest = {
  top: CommandView[];
  active: number;
  total: number;
  uses: number;
};

function digest(cmds: CommandView[]): CommandDigest {
  const active = cmds.filter((c) => c.is_active);
  return {
    top: [...active].toSorted((a, b) => usesCount(b.uses) - usesCount(a.uses)).slice(0, 3),
    active: active.length,
    total: cmds.length,
    uses: cmds.reduce((n, c) => n + usesCount(c.uses), 0)
  };
}

const demoDigest: CommandDigest = digest([
  { name: 'bagel', response: '{user} tosses a warm bagel to {target}. Toasty.', is_active: true, uses: '1.2k' },
  { name: 'lurk', response: '{user} fades into the shadows. Thanks for the lurk.', is_active: true, uses: '521' },
  { name: 'uptime', response: '@{user} the stream has been live for {uptime} 🥯', is_active: true, uses: '412' },
  { name: 'socials', response: 'Follow along → twitch.tv/itsmavey', is_active: true, uses: '288' },
  { name: 'uptime-debug', response: 'node={node}', is_active: false, uses: '14' }
]);

// Modules at a glance: enabled count over the user-facing catalog.
export type ModuleDigest = { on: number; total: number };

// Who can reach this dashboard: consumed delegation grants.
export type ShareDigest = { people: number; pending: number };

export const load: PageServerLoad = ({ locals }) => {
  const uid = locals.session?.user_id ?? 'demo';

  // Return the RPCs as unawaited promises so SvelteKit streams them: the page
  // shell flushes immediately and each section hydrates when its round trip
  // lands, instead of blocking SSR (and the post-login redirect) on NATS.
  const conn: Promise<ConnState> =
    env.DEMO === '1'
      ? Promise.resolve({ enabled: true, receiving: true, status: 'vip', subState: 'ok', subError: '' })
      : connState(uid);

  // Command digest for the stat cards + top strip. Cache-backed (same fabric
  // entry as the commands page) and optional: a failure just hides the strip.
  const commands: Promise<CommandDigest> =
    env.DEMO === '1'
      ? Promise.resolve(demoDigest)
      : listCommands(uid)
          .then(digest)
          .catch(() => ({ top: [], active: 0, total: 0, uses: 0 }) as CommandDigest);

  const catalogIds = new Set(MODULE_CATALOG.map((m) => m.id));
  const modules: Promise<ModuleDigest> =
    env.DEMO === '1'
      ? Promise.resolve({ on: 1, total: MODULE_CATALOG.length })
      : listModules(uid)
          .then((rows) => ({
            on: rows.filter((r) => catalogIds.has(r.name) && r.is_enabled).length,
            total: MODULE_CATALOG.length
          }))
          .catch(() => ({ on: 0, total: MODULE_CATALOG.length }));

  // Delegation shares only exist for owners; a delegate browsing the owner's
  // board doesn't own grants, so show zero rather than erroring.
  const shares: Promise<ShareDigest> =
    env.DEMO === '1'
      ? Promise.resolve({ people: 1, pending: 1 })
      : delegationList(uid)
          .then((grants) => ({
            people: grants.filter((g) => g.consumed).length,
            pending: grants.filter((g) => !g.consumed).length
          }))
          .catch(() => ({ people: 0, pending: 0 }));

  return { conn, commands, modules, shares };
};

export const actions: Actions = {
  // Enable: mark the channel active and create the EventSub subs. This is a
  // plain create (enabled=true), not a reconnect: a first-time or re-enable has
  // nothing to drop, and the creates are 409-idempotent, so drop-then-recreate
  // would only add a needless delete pass and reset Twitch's conduit routing
  // propagation for the fresh channel.chat.message sub. Use restart (below) for
  // an intentional drop+recreate of an already-connected channel.
  enable: async ({ locals }) => {
    if (locals.session?.delegate_of) return fail(403);
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    try {
      await setActive(uid, true);
      await publishEventSub(uid, true);
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
  },
  // Onboarded: mark the user as having completed the onboarding flow.
  onboarded: async ({ locals }) => {
    if (locals.session?.delegate_of) return fail(403);
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    try {
      await setOnboarded(uid, true);
      return { ok: true, action: 'onboarded' };
    } catch {
      return fail(502, { error: 'onboarded failed' });
    }
  }
};
