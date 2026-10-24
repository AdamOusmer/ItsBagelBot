// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { usesCount, compareUses } from '@bagel/kit/uses';
import { betaLocked } from '@bagel/kit';
import type { CommandView } from '@bagel/kit/types';
import { MODULE_CATALOG, catalogIndexable } from '@bagel/kit/types';
import {
  hasGrant,
  accountState,
  setActive,
  retryLifecycle,
  publishEventSub,
  publishEventSubReconnect,
  channelSubState,
  type ChannelSubState,
  auditDashboardImpersonation,
  delegationList,
} from '$lib/server/services';
import { listCommands, listModules } from '$lib/server/commands-store';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { connectionUiState, type ConnSignals, type ConnUi } from '@bagel/kit/connection-state';
import { fail, redirect } from '@sveltejs/kit';
import { overviewLanes } from '$lib/server/overview-lanes';
import { broadcasterPremium } from '$lib/server/module-gate';
import { twitchScopeGap } from '$lib/server/oauth';
import { isOwnerSession } from '$lib/server/owner';
import { shareRecent } from '$lib/server/recent-share';

const DEMO = dev && env.DEMO === '1';

export type ConnData = { signals: ConnSignals; ui: ConnUi };

function settled<T>(r: PromiseSettledResult<T>): T | undefined {
  return r.status === 'fulfilled' ? r.value : undefined;
}

const VERIFY_REUSE_MS = 30_000;

const recentVerify = shareRecent<Promise<ChannelSubState | undefined>>(VERIFY_REUSE_MS);

async function verifiedIfUnknown(uid: string, stored: ChannelSubState | undefined) {
  if (stored && stored.state !== 'unknown') return stored;
  return recentVerify(uid, () => channelSubState(uid, { verify: true, repair: true }).catch(() => stored));
}

async function connState(uid: string): Promise<ConnData> {
  const [grant, state, sub] = await Promise.allSettled([
    hasGrant(uid),
    accountState(uid),
    channelSubState(uid)
  ]);
  const account = settled(state);
  const subHealth = await verifiedIfUnknown(uid, settled(sub));

  const grantOk: boolean | 'unknown' = grant.status === 'fulfilled' ? grant.value === true : 'unknown';
  const active: boolean | 'unknown' =
    account ? account.active === true : 'unknown';

  const subState = subHealth?.state ?? 'unknown';

  const signals: ConnSignals = {
    lifecycle: account?.lifecycle,
    lifecycleRevision: account?.lifecycleRevision,
    accountCreatedAt: account?.accountCreatedAt,
    grant: grantOk,
    active,
    status: account?.status ?? 'unknown',
    sub: subState
  };
  return { signals, ui: connectionUiState(signals) };
}

export type CommandDigest = {
  top: CommandView[];
  active: number;
  total: number;
  uses: string;
  ok: boolean;
};

function digest(cmds: CommandView[]): Omit<CommandDigest, 'ok'> {
  const active = cmds.filter((c) => c.is_active);
  return {
    top: [...active].toSorted((a, b) => compareUses(b, a)).slice(0, 3),
    active: active.length,
    total: cmds.length,
    uses: cmds.reduce((n, c) => n + usesCount(c), 0n).toString()
  };
}

export type ModuleDigest = { on: number; switchedOn: number; total: number; ok: boolean };

export type ShareDigest = { people: number; pending: number; ok: boolean };

function demoOr<T>(pick: (m: typeof import('$lib/server/demo-data')) => T, real: () => Promise<T>): Promise<T> {
  return DEMO ? import('$lib/server/demo-data').then(pick) : real();
}

function commandDigest(uid: string): Promise<CommandDigest> {
  return listCommands(uid)
    .then((c) => ({ ...digest(c), ok: true }))
    .catch(() => ({ top: [], active: 0, total: 0, uses: '0', ok: false }));
}

function moduleDigest(uid: string, premium: boolean): Promise<ModuleDigest> {
  const visibleCatalog = MODULE_CATALOG.filter((m) => catalogIndexable(m));
  return listModules(uid)
    .then((rows) => {
      const byName = new Map(rows.map((row) => [row.name, row]));
      return {
        on: visibleCatalog.filter((def) =>
          def.toggleable === false ? true : (byName.get(def.id)?.is_enabled ?? def.defaultEnabled)
        ).length,
        switchedOn: visibleCatalog.filter((def) =>
          def.toggleable !== false && !betaLocked(def, premium) && (byName.get(def.id)?.is_enabled ?? def.defaultEnabled)
        ).length,
        total: visibleCatalog.length,
        ok: true
      };
    })
    .catch(() => ({ on: 0, switchedOn: 0, total: visibleCatalog.length, ok: false }));
}

function shareDigest(uid: string): Promise<ShareDigest> {
  return delegationList(uid)
    .then((grants) => ({
      people: grants.filter((g) => g.consumed).length,
      pending: grants.filter((g) => !g.consumed).length,
      ok: true
    }))
    .catch(() => ({ people: 0, pending: 0, ok: false }));
}

function delegateLanding(s: App.Locals['session']): string | null {
  if (!s?.delegate_of) return null;
  const first = s.sections?.[0];
  return first ? `/${first}` : '/delegate/exit';
}

function ownerNotOnboarded(locals: App.Locals): boolean {
  const s = locals.session;
  const gate = locals.accountState;
  if (!s || s.impersonator_id) return false;
  if (!gate || !('value' in gate)) return false;
  return !gate.value.onboarded;
}

function ownerScopeGap(s: App.Locals['session']): boolean {
  return isOwnerSession(s) && twitchScopeGap(s.scopes);
}

async function needsTour(locals: App.Locals, commands: Promise<CommandDigest>): Promise<boolean> {
  if (!ownerNotOnboarded(locals)) return false;
  const cd = await commands;
  return cd.ok && cd.total === 0;
}

function boardUid(locals: App.Locals): string | null {
  return locals.session?.user_id ?? (DEMO ? 'demo' : null);
}

async function tourFor(locals: App.Locals, url: URL, commands: Promise<CommandDigest>): Promise<string | null> {
  if (url.searchParams.get('welcome') === '1') return '/welcome';
  return (await needsTour(locals, commands)) ? '/welcome' : null;
}

export const load: PageServerLoad = async ({ locals, url }) => {
  const landing = delegateLanding(locals.session);
  if (landing) throw redirect(302, landing);
  const uid = boardUid(locals);
  if (!uid) throw redirect(302, '/login');
  const commands = demoOr((m) => m.demoCommandDigest(digest), () => commandDigest(uid));
  const tour = await tourFor(locals, url, commands);
  if (tour) throw redirect(302, tour);
  return {
    conn: demoOr<ConnData>((m) => m.demoConn(connectionUiState), () => connState(uid)),
    commands,
    modules: demoOr<ModuleDigest>((m) => m.demoModuleDigest, () => broadcasterPremium(locals).then((premium) => moduleDigest(uid, premium))),
    shares: demoOr<ShareDigest>((m) => m.demoShareDigest, () => shareDigest(uid)),
    twitchScopeGap: ownerScopeGap(locals.session),

    ...overviewLanes(uid)
  };
};

type OwnerAction = {
  name: string;
  audit: boolean;
  run: (uid: string) => Promise<unknown>;
};

function ownerAction({ name, audit, run }: OwnerAction) {
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
  retry: ownerAction({ name: 'retry', audit: true, run: (uid) => retryLifecycle({broadcaster_user_id:uid}) }),
  enableAndVerify: ownerAction({ name: 'enableAndVerify', audit: true, run: async(uid)=>{ await setActive(uid,true); await retryLifecycle({broadcaster_user_id:uid}); } }),
  // A plain create, not a reconnect: drop-then-recreate resets Twitch's conduit routing.
  enable: ownerAction({
    name: 'enable',
    audit: true,
    run: async (uid) => {
      await setActive(uid, true);
      await publishEventSub(uid, true);

    }
  }),
  restart: ownerAction({ name: 'restart', audit: true, run: (uid) => publishEventSubReconnect(uid) }),
  disconnect: ownerAction({
    name: 'disconnect',
    audit: true,
    run: async (uid) => {
      await publishEventSub(uid, false);
      await setActive(uid, false);
    }
  })
};
