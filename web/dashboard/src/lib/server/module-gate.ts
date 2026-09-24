// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/environment';
import { redirect, type RequestEvent } from '@sveltejs/kit';
import { MODULE_CATALOG, betaLocked, moduleDef, moduleDelegateSections, type ModuleDef } from '@bagel/kit';
import type { Session } from '$lib/server/session';
import { accountState, type AccountState } from '$lib/server/services';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
const DEMO = dev && process.env.DEMO === '1';

export function delegateCanOpen(def: ModuleDef, session: Session | null | undefined): boolean {
  if (!session?.delegate_of || !def.href) return true;
  const sections = session.sections ?? [];
  return moduleDelegateSections(def).some((sec) => sections.includes(sec));
}

export function gateModulePage(session: Session | null | undefined, moduleId: string): void {
  const def = moduleDef(moduleId);
  if (!def || !delegateCanOpen(def, session)) throw redirect(302, '/');
}

function premiumStatus(acc: AccountState | null | undefined): boolean {
  return !!acc && (acc.status === 'vip' || acc.status === 'paid');
}

export async function broadcasterPremium(locals: App.Locals): Promise<boolean> {
  if (DEMO) return premiumStatus((await import('$lib/server/demo-data')).demoAccountState);
  const s = locals.session;
  if (!s) return false;
  if (s.delegate_of) return accountState(s.delegate_of).then(premiumStatus).catch(() => false);
  const cached = locals.accountState;
  if (cached && 'value' in cached) return premiumStatus(cached.value);
  return accountState(s.user_id).then(premiumStatus).catch(() => false);
}

export async function moduleLocked(locals: App.Locals, def: ModuleDef): Promise<boolean> {
  if (!def.beta) return false;
  return betaLocked(def, await broadcasterPremium(locals));
}

export function betaRouteDef(pathname: string): ModuleDef | undefined {
  return MODULE_CATALOG.find((d) => d.beta && d.href && (pathname === d.href || pathname.startsWith(d.href + '/')));
}

export async function assertBetaRouteOpen(event: RequestEvent): Promise<void> {
  const def = betaRouteDef(event.url.pathname);
  if (!def || !(await moduleLocked(event.locals, def))) return;
  if (def.section) return;
  throw redirect(303, '/modules');
}

export async function assertModuleUnlocked(locals: App.Locals, def: ModuleDef): Promise<boolean> {
  return !(await moduleLocked(locals, def));
}

export function assertModuleWritable(session: Session | null | undefined, def: ModuleDef): boolean {
  return !def.hidden && delegateCanOpen(def, session);
}
