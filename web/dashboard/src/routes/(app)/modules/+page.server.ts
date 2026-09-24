// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { ModuleState } from '@bagel/kit';
import { MODULE_CATALOG, betaLocked, catalogIndexable, moduleDef } from '@bagel/kit';
import { listModules, type ModuleView } from '$lib/server/commands-store';
import { setModuleEnabled } from '$lib/server/module-blob';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/kit/server/logger';
import { assertModuleWritable, broadcasterPremium, delegateCanOpen, moduleLocked } from '$lib/server/module-gate';
import { disableChildren } from '$lib/server/module-parent';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';
import { actionError } from '$lib/server/action-errors';

const DEMO = dev && env.DEMO === '1';

function gateModules(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('modules')) {
    throw redirect(302, '/');
  }
}

function asConfig(raw: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (raw && typeof raw === 'object') {
    for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
      out[k] = v == null ? '' : String(v);
    }
  }
  return out;
}

function merge(rows: ModuleView[], session: Session | null | undefined, premium: boolean): ModuleState[] {
  const byName = new Map(rows.map((r) => [r.name, r]));
  return MODULE_CATALOG.filter((def) => catalogIndexable(def) && delegateCanOpen(def, session)).map((def) => {
    const row = byName.get(def.id);
    return {
      def,
      enabled: def.toggleable === false ? true : row ? row.is_enabled : def.defaultEnabled,
      config: asConfig(row?.configs),
      locked: betaLocked(def, premium)
    };
  });
}

export const load: PageServerLoad = async ({ locals }) => {
  gateModules(locals.session);
  const uid = effectiveId(locals.session);
  const premium = await broadcasterPremium(locals);
  if (DEMO) return { modules: merge([], locals.session, premium) };
  try {
    return { modules: merge(await listModules(uid), locals.session, premium) };
  } catch {
    return { modules: merge([], locals.session, premium), degraded: true };
  }
};

type ToggleTarget = { denied: ReturnType<typeof fail> } | { uid: string };

async function resolveToggle(name: string, locals: App.Locals): Promise<ToggleTarget> {
  const session = locals.session;
  const def = moduleDef(name);
  if (!def || def.toggleable === false || def.parent) return { denied: fail(400, { ok: false, error: actionError(locals.locale, 'Unknown module.') }) };
  if (!assertModuleWritable(session, def)) return { denied: fail(403, { ok: false, error: actionError(locals.locale, 'Not allowed.') }) };
  if (await moduleLocked(locals, def)) return { denied: fail(403, { ok: false, error: actionError(locals.locale, 'Premium only while in beta.') }) };
  return { uid: effectiveId(session) };
}

async function actionContext({ request, locals }: { request: Request; locals: App.Locals }) {
  gateModules(locals.session);
  if (!DEMO && !locals.session) return null;
  return { session: locals.session, form: await request.formData() };
}

export const actions: Actions = {
  toggle: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return fail(401, { ok: false, error: actionError(event.locals.locale, 'Not signed in.') });

    const f = ctx.form;
    const name = String(f.get('name') ?? '');
    const target = await resolveToggle(name, event.locals);
    if ('denied' in target) return target.denied;
    const enabled = f.get('is_enabled') === 'on';

    if (DEMO) return { ok: true, name, enabled };

    return flipModule({ name, uid: target.uid, enabled }, ctx.session);
  }
};

// Write the stored config back untouched: rebuilding it from the form stringifies nested blobs.
async function flipModule(
  flip: { name: string; uid: string; enabled: boolean },
  session: Session | null | undefined
) {
  const { name, uid, enabled } = flip;
  try {
    await setModuleEnabled(uid, name, enabled);
    if (!enabled) await disableChildren(uid, name);
  } catch (e) {
    logger.error({ err: e }, `[modules] toggle ${name} failed`);
    return fail(400, { ok: false });
  }

  auditDashboardImpersonation(session, 'module:toggle', `${name}=${enabled}`);
  return { ok: true, name, enabled };
}
