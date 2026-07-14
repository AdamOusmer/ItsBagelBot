// Delegate gate for the bespoke module pages (quotes, timers, govee, ...).
// Which delegation grants open a page comes from the module catalog
// (moduleDelegateSections), the same source guard.ts uses for route scoping —
// one place to declare it, so the route guard and the page gates can't drift
// and a new bespoke page needs no bespoke gate. This stays as defense in depth
// under the guard: pages call it from load AND every action.
import { redirect } from '@sveltejs/kit';
import { moduleDef, moduleDelegateSections, type ModuleDef } from '@bagel/shared';
import type { Session } from '$lib/server/session';

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
