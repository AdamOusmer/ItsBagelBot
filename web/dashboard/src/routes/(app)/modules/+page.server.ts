// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { toWire, type ModuleStateWire } from '$lib/module-states';
import { MODULE_CATALOG, betaLocked, catalogIndexable, moduleDef, type ModuleDef } from '@bagel/kit';
import { listModules, type ModuleView } from '$lib/server/commands-store';
import { setModuleEnabled } from '$lib/server/module-blob';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/kit/server/logger';
import { broadcasterPremium, delegateCanOpen, gateSection, moduleWriteDenial } from '$lib/server/module-gate';
import { moduleEditState } from '$lib/server/module-edit-state';
import { disableChildren } from '$lib/server/module-parent';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { signedInAction } from '$lib/server/action-context';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';
import { actionError } from '$lib/server/action-errors';

const DEMO = dev && env.DEMO === '1';

function merge(rows: ModuleView[], session: Session | null | undefined, premium: boolean): ModuleStateWire[] {
  const byName = new Map(rows.map((r) => [r.name, r]));
  return MODULE_CATALOG.filter((def) => catalogIndexable(def) && delegateCanOpen(def, session)).map((def) => {
    const row = byName.get(def.id);
    const { config, revision } = moduleEditState(row);
    return toWire({
      def,
      enabled: def.toggleable === false ? true : row ? row.is_enabled : def.defaultEnabled,
      revision,
      config,
      locked: betaLocked(def, premium)
    });
  });
}

export const load: PageServerLoad = async ({ locals }) => {
  gateSection(locals.session, 'modules');
  const uid = effectiveId(locals.session);
  const premium = await broadcasterPremium(locals);
  if (DEMO) return { modules: merge([], locals.session, premium) };
  try {
    return { modules: merge(await listModules(uid), locals.session, premium) };
  } catch {
    return { modules: merge([], locals.session, premium), degraded: true };
  }
};

function userToggleable(def: ModuleDef | undefined): def is ModuleDef {
  return def !== undefined && def.toggleable !== false && !def.parent;
}

const modulesAction = signedInAction((session) => gateSection(session, 'modules'), DEMO);

export const actions: Actions = {
  toggle: modulesAction(async (ctx, { locals }) => {
    const name = String(ctx.form.get('name') ?? '');
    const def = moduleDef(name);
    if (!userToggleable(def)) return fail(400, { ok: false, error: actionError(ctx.locale, 'Unknown module.') });
    const denied = await moduleWriteDenial(locals, def);
    if (denied) return denied;
    const enabled = ctx.form.get('is_enabled') === 'on';

    if (DEMO) return { ok: true, name, enabled };

    return flipModule({ name, uid: ctx.uid, enabled }, ctx.session);
  })
};

// Write the stored config back untouched: rebuilding it from the form stringifies nested blobs.
async function flipModule(
  flip: { name: string; uid: string; enabled: boolean },
  session: Session | null | undefined
) {
  const { name, uid, enabled } = flip;
  try {
    const saved = await setModuleEnabled(uid, name, enabled);
    if (!enabled) await disableChildren(uid, name);
    auditDashboardImpersonation(session, 'module:toggle', `${name}=${enabled}`);
    return { ok: true, name, enabled, revision: saved?.revision };
  } catch (e) {
    logger.error({ err: e }, `[modules] toggle ${name} failed`);
    return fail(400, { ok: false });
  }
}
