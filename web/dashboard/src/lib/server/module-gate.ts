// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail, redirect, type ActionFailure, type RequestEvent } from '@sveltejs/kit';
import { MODULE_CATALOG, betaLocked, moduleDef, moduleDelegateSections, type ModuleDef } from '@bagel/kit';
import type { Session } from '#lib/server/session';
import { accountState, type AccountState } from '#lib/server/services';
import { actionError } from '#lib/server/action-errors';

export function delegateCanOpen(def: ModuleDef, session: Session | null | undefined): boolean {
  if (!session?.delegate_of || !def.href) return true;
  const sections = session.sections ?? [];
  return moduleDelegateSections(def).some((sec) => sections.includes(sec));
}

export function gateSection(session: Session | null | undefined, section: string): void {
  if (session?.delegate_of && !(session.sections ?? []).includes(section)) {
    throw redirect(302, '/');
  }
}

export function gateModulePage(session: Session | null | undefined, moduleId: string): void {
  const def = moduleDef(moduleId);
  if (!def || !delegateCanOpen(def, session)) throw redirect(302, '/');
}

function premiumStatus(acc: AccountState | null | undefined): boolean {
  return !!acc && (acc.status === 'vip' || acc.status === 'paid');
}

export async function accountPremium(userId: string): Promise<boolean> {
  return accountState(userId).then(premiumStatus).catch(() => false);
}

export async function broadcasterPremium(locals: App.Locals): Promise<boolean> {
  const s = locals.session;
  if (!s) return false;
  if (s.delegate_of) return accountPremium(s.delegate_of);
  const cached = locals.accountState;
  if (cached && 'value' in cached) return premiumStatus(cached.value);
  return accountPremium(s.user_id);
}

export async function moduleLocked(locals: App.Locals, def: ModuleDef): Promise<boolean> {
  if (!def.beta) return false;
  return betaLocked(def, await broadcasterPremium(locals));
}

export function betaRouteDef(pathname: string): ModuleDef | undefined {
  return MODULE_CATALOG.find((d) => d.beta && d.href && (pathname === d.href || pathname.startsWith(d.href + '/')));
}

const routePath = (routeId: string | null) => (routeId ?? '').replace(/\/\([^/]*\)/g, '');

export async function assertBetaRouteOpen(event: RequestEvent): Promise<void> {
  const def = betaRouteDef(routePath(event.route.id));
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

export async function moduleWriteDenial(
  locals: App.Locals,
  def: ModuleDef
): Promise<ActionFailure<{ ok: false; error: string }> | null> {
  if (!assertModuleWritable(locals.session, def)) return fail(403, { ok: false, error: actionError(locals.locale, 'Not allowed.') });
  if (await moduleLocked(locals, def)) return fail(403, { ok: false, error: actionError(locals.locale, 'Premium only while in beta.') });
  return null;
}
