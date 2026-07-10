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

function settled<T>(r: PromiseSettledResult<T>): T | undefined {
  return r.status === 'fulfilled' ? r.value : undefined;
}

// One enable publish per user per window, per replica. The enable action and
// the self-heal below both publish the same job, and right after a connect
// click the registry still reads 'unenrolled' until the worker flips it to
// 'pending' — without this guard every page load and substate poll in that
// window fires another full enroll (each worth ~13-24 reserved Helix calls;
// two back-to-back full enrolls observed 2026-07-10). Per-replica memory is
// enough: the worker-side enroll cooldown is the fleet-wide guard, this only
// stops one replica re-publishing what it just sent.
const ENABLE_PUBLISH_WINDOW_MS = 120_000;
const recentEnables = new Map<string, number>();

function pruneEnableStamps(now: number): void {
  for (const [k, t] of recentEnables) {
    if (now - t >= ENABLE_PUBLISH_WINDOW_MS) recentEnables.delete(k);
  }
}

// Self-heal: a channel the users service says is active but outgress has no
// enrollment for (fresh signup, or one predating auto-enroll) gets its
// EventSub enable job published right here on page load. Safe to repeat:
// outgress single-flights the enroll and creates are 409-idempotent. Only
// 'unenrolled' triggers this — 'unknown' (outgress RPC down) must not spam
// enables. Reports 'pending' so the UI shows the enroll in flight; the
// substate poll takes over with the real outcome.
function healSubState(conn: {
  uid: string;
  active: boolean;
  state: ChannelSubState['state'];
}): ChannelSubState['state'] {
  const { uid, active, state } = conn;
  if (state !== 'unenrolled' || uid === 'demo') return state;
  if (!active) return state;

  // One publish per window (see recentEnables above): right after a connect
  // click the registry still reads 'unenrolled', so without the stamp every
  // load and poll here would fire another full enroll.
  const now = Date.now();
  const stamped = recentEnables.get(uid);
  if (stamped !== undefined && now - stamped < ENABLE_PUBLISH_WINDOW_MS) return 'pending';
  recentEnables.set(uid, now);
  if (recentEnables.size > 1024) pruneEnableStamps(now);

  // A failed publish must not suppress the next heal for the whole window.
  publishEventSub(uid, true).catch(() => recentEnables.delete(uid));
  return 'pending';
}

// Resolve the bot connection state in one round trip (grant presence + the
// coalesced active/tier state_get + channel enroll state). allSettled keeps a
// slow or down responder from failing the whole render.
//
// "Receiving" is grounded in outgress's own enroll state, not just the users
// service's active flag: is_active defaults to true at signup, so trusting it
// alone showed brand-new channels as connected while they had zero EventSub
// subscriptions (and hid the only button that would have created them).
async function connState(uid: string): Promise<ConnState> {
  const [grant, state, sub] = await Promise.allSettled([
    hasGrant(uid),
    accountState(uid),
    channelSubState(uid)
  ]);
  const account = settled(state);
  const subHealth = settled(sub);
  const enabled = settled(grant) === true;
  const active = enabled && account?.active === true;
  const subState = healSubState({ uid, active, state: subHealth?.state ?? 'unknown' });
  return {
    enabled,
    receiving: active && subState !== 'unenrolled',
    status: account?.status ?? 'free',
    subState,
    subError: subHealth?.error ?? ''
  };
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
  { name: 'uptime', response: '{user} the stream has been live for {uptime}', is_active: true, uses: '412' },
  { name: 'socials', response: 'Follow along → twitch.tv/itsmavey', is_active: true, uses: '288' },
  { name: 'uptime-debug', response: 'node={node}', is_active: false, uses: '14' }
]);

// Modules at a glance: enabled count over the user-facing catalog.
export type ModuleDigest = { on: number; total: number };

// Who can reach this dashboard: consumed delegation grants.
export type ShareDigest = { people: number; pending: number };

// demoOr streams either the demo fixture or the real RPC as an unawaited
// promise so SvelteKit streams it: the page shell flushes immediately and each
// section hydrates when its round trip lands, instead of blocking SSR (and the
// post-login redirect) on NATS.
function demoOr<T>(demo: T, real: () => Promise<T>): Promise<T> {
  return env.DEMO === '1' ? Promise.resolve(demo) : real();
}

// commandDigest feeds the stat cards + top strip. Cache-backed (same fabric
// entry as the commands page) and optional: a failure just hides the strip.
function commandDigest(uid: string): Promise<CommandDigest> {
  return listCommands(uid)
    .then(digest)
    .catch(() => ({ top: [], active: 0, total: 0, uses: 0 }));
}

function moduleDigest(uid: string): Promise<ModuleDigest> {
  const catalogIds = new Set(MODULE_CATALOG.map((m) => m.id));
  return listModules(uid)
    .then((rows) => ({
      on: rows.filter((r) => catalogIds.has(r.name) && r.is_enabled).length,
      total: MODULE_CATALOG.length
    }))
    .catch(() => ({ on: 0, total: MODULE_CATALOG.length }));
}

// Delegation shares only exist for owners; a delegate browsing the owner's
// board doesn't own grants, so show zero rather than erroring.
function shareDigest(uid: string): Promise<ShareDigest> {
  return delegationList(uid)
    .then((grants) => ({
      people: grants.filter((g) => g.consumed).length,
      pending: grants.filter((g) => !g.consumed).length
    }))
    .catch(() => ({ people: 0, pending: 0 }));
}

export const load: PageServerLoad = ({ locals }) => {
  const uid = locals.session?.user_id ?? 'demo';
  return {
    conn: demoOr<ConnState>(
      { enabled: true, receiving: true, status: 'vip', subState: 'ok', subError: '' },
      () => connState(uid)
    ),
    commands: demoOr(demoDigest, () => commandDigest(uid)),
    modules: demoOr({ on: 1, total: MODULE_CATALOG.length }, () => moduleDigest(uid)),
    shares: demoOr({ people: 1, pending: 1 }, () => shareDigest(uid))
  };
};

// ownerAction wraps the shared shape of every home-page action: owners only (a
// delegate browsing the owner's board cannot flip the connection), then the
// RPC sequence, then the audit trail; any failure maps to a 502 the client
// toasts. onboarded skips the audit (it is not an impersonatable act).
function ownerAction(name: string, audit: boolean, run: (uid: string) => Promise<unknown>) {
  return async ({ locals }: { locals: App.Locals }) => {
    if (locals.session?.delegate_of) return fail(403);
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    try {
      await run(uid);
      if (audit) auditDashboardImpersonation(locals.session, name);
      return { ok: true, action: name };
    } catch {
      return fail(502, { error: `${name} failed` });
    }
  };
}

export const actions: Actions = {
  // Enable: mark the channel active and create the EventSub subs. This is a
  // plain create (enabled=true), not a reconnect: a first-time or re-enable has
  // nothing to drop, and the creates are 409-idempotent, so drop-then-recreate
  // would only add a needless delete pass and reset Twitch's conduit routing
  // propagation for the fresh channel.chat.message sub. Use restart (below) for
  // an intentional drop+recreate of an already-connected channel.
  enable: ownerAction('enable', true, async (uid) => {
    await setActive(uid, true);
    await publishEventSub(uid, true);
    // The loads/polls that follow this action still read 'unenrolled' until
    // the worker picks the job up; stamp the publish so the self-heal doesn't
    // immediately fire a duplicate enroll.
    recentEnables.set(uid, Date.now());
  }),
  // Restart: atomic drop + recreate of EventSub subscriptions (stays active).
  restart: ownerAction('restart', true, (uid) => publishEventSubReconnect(uid)),
  // Disconnect: delete the subscriptions and mark inactive (grant kept).
  disconnect: ownerAction('disconnect', true, async (uid) => {
    await publishEventSub(uid, false);
    await setActive(uid, false);
  }),
  // Onboarded: mark the user as having completed the onboarding flow.
  onboarded: ownerAction('onboarded', false, (uid) => setOnboarded(uid, true))
};
