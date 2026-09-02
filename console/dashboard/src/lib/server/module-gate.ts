// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Delegate gate for the bespoke module pages (quotes, timers, govee, ...).
// Which delegation grants open a page comes from the module catalog
// (moduleDelegateSections), the same source guard.ts uses for route scoping —
// one place to declare it, so the route guard and the page gates can't drift
// and a new bespoke page needs no bespoke gate. This stays as defense in depth
// under the guard: pages call it from load AND every action.
import { dev } from '$app/environment';
import { redirect } from '@sveltejs/kit';
import { MODULE_CATALOG, betaLocked, moduleDef, moduleDelegateSections, type ModuleDef } from '@bagel/shared';
import type { Session } from '$lib/server/session';
import { accountState, type AccountState } from '$lib/server/services';

// process.env, not $env/dynamic/private: guard.ts imports this module and sits
// in the boot import graph (see the note there).
const DEMO = dev && process.env.DEMO === '1';

// delegateCanOpen reports whether the session may open a module's page.
// Owners and normal logins always may, as may anyone for a module without a
// bespoke page (its generic /modules/[id] page rides the modules grant); a
// delegate needs one of the def's sections. Also drives the tile grid, which
// hides modules a delegate cannot open.
export function delegateCanOpen(def: ModuleDef, session: Session | null | undefined): boolean {
  if (!session?.delegate_of || !def.href) return true;
  const sections = session.sections ?? [];
  return moduleDelegateSections(def).some((sec) => sections.includes(sec));
}

// gateModulePage throws unless the session may open the module's page.
export function gateModulePage(session: Session | null | undefined, moduleId: string): void {
  const def = moduleDef(moduleId);
  if (!def || !delegateCanOpen(def, session)) throw redirect(302, '/');
}

function premiumStatus(acc: AccountState | null | undefined): boolean {
  return !!acc && (acc.status === 'vip' || acc.status === 'paid');
}

// broadcasterPremium reports whether the board being edited belongs to a
// premium channel. It is the BROADCASTER's tier, never the visitor's: a
// delegate on a premium board gets the beta features that board paid for,
// matching sesame, which gates on the lane the broadcaster's events ride. A
// normal login reuses the account read the guard already made for this
// request; a delegate costs one extra RPC (the guard only reads the
// delegate's own account). A failed read counts as not premium so a blip can
// only ever lock, never unlock.
export async function broadcasterPremium(locals: App.Locals): Promise<boolean> {
  if (DEMO) return premiumStatus((await import('$lib/server/demo-data')).demoAccountState);
  const s = locals.session;
  if (!s) return false;
  if (s.delegate_of) return accountState(s.delegate_of).then(premiumStatus).catch(() => false);
  const cached = locals.accountState;
  if (cached && 'value' in cached) return premiumStatus(cached.value);
  return accountState(s.user_id).then(premiumStatus).catch(() => false);
}

// moduleLocked reports whether a beta module is closed to this board. Cheap
// for the common case: only a def flagged beta pays the tier lookup.
export async function moduleLocked(locals: App.Locals, def: ModuleDef): Promise<boolean> {
  if (!def.beta) return false;
  return betaLocked(def, await broadcasterPremium(locals));
}

// betaRouteDef finds the beta module whose bespoke page owns this pathname,
// if any, so the route guard can lock the page and its actions in one place
// instead of each bespoke page growing its own tier check.
export function betaRouteDef(pathname: string): ModuleDef | undefined {
  return MODULE_CATALOG.find((d) => d.beta && d.href && (pathname === d.href || pathname.startsWith(d.href + '/')));
}

// assertModuleWritable reports whether a write action (toggle, save, patch)
// may touch this module's stored row. It folds together the same two checks
// the read paths already make (delegateCanOpen's scope check, and the
// def.hidden rejection the [id] load performs) so a write gate can never end
// up looser than its matching read gate: gateModules() alone only knows the
// 'modules' section, not that a module like channel points is its own grant.
export function assertModuleWritable(session: Session | null | undefined, def: ModuleDef): boolean {
  return !def.hidden && delegateCanOpen(def, session);
}
