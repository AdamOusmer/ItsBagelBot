// Delegate gate for the bespoke module pages (quotes, timers, govee, ...).
// Which delegation grants open a page comes from the module catalog
// (moduleDelegateSections), the same source guard.ts uses for route scoping —
// one place to declare it, so the route guard and the page gates can't drift
// and a new bespoke page needs no bespoke gate. This stays as defense in depth
// under the guard: pages call it from load AND every action.
import { redirect } from '@sveltejs/kit';
import { moduleDef, moduleDelegateSections } from '@bagel/shared';
import type { Session } from '$lib/server/session';

// gateModulePage throws unless the session may open the module's page: owners
// and normal logins always pass; a delegate needs one of the def's sections.
export function gateModulePage(session: Session | null | undefined, moduleId: string): void {
  if (!session?.delegate_of) return;
  const def = moduleDef(moduleId);
  const sections = session.sections ?? [];
  if (!def || !moduleDelegateSections(def).some((sec) => sections.includes(sec))) {
    throw redirect(302, '/');
  }
}
