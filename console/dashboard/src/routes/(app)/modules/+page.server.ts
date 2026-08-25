// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { ModuleState } from '@bagel/shared';
import { MODULE_CATALOG, catalogIndexable, moduleDef } from '@bagel/shared';
import { listModules, upsertModule, type ModuleView } from '$lib/server/commands-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import { assertModuleWritable, delegateCanOpen } from '$lib/server/module-gate';
import { disableChildren } from '$lib/server/module-parent';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// A delegate needs the 'modules' section to be here; a normal login always may.
function gateModules(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('modules')) {
    throw redirect(302, '/');
  }
}

// Coerce a stored module config blob into a flat string map.
function asConfig(raw: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (raw && typeof raw === 'object') {
    for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
      out[k] = v == null ? '' : String(v);
    }
  }
  return out;
}

// Merge the catalog (the modules we expose) with the broadcaster's stored rows.
// Modules absent from the catalog (system, bagel, ...) are never surfaced, and
// a delegate's grid drops tiles their grant cannot open (delegateCanOpen) —
// such a tile would only bounce off the route guard. Owners see everything.
function merge(rows: ModuleView[], session: Session | null | undefined): ModuleState[] {
  const byName = new Map(rows.map((r) => [r.name, r]));
  return MODULE_CATALOG.filter((def) => catalogIndexable(def) && delegateCanOpen(def, session)).map((def) => {
    const row = byName.get(def.id);
    return {
      def,
      enabled: def.toggleable === false ? true : row ? row.is_enabled : def.defaultEnabled,
      config: asConfig(row?.configs)
    };
  });
}

// Tiles read state for the status + quick toggle; each module's own page owns the
// reply builder and per-reply toggles.
export const load: PageServerLoad = async ({ locals }) => {
  gateModules(locals.session);
  const uid = effectiveId(locals.session);
  if (DEMO) return { modules: merge([], locals.session) };
  try {
    return { modules: merge(await listModules(uid), locals.session) };
  } catch {
    return { modules: merge([], locals.session), degraded: true };
  }
};

// resolveToggle gates the tile toggle against the module it actually names.
// Rejections come back as `denied` for the action to hand straight back.
// gateModules only proves the 'modules' section, so the per-module check
// matters here: channel points is its own delegation grant and would
// otherwise flip through this generic toggle.
type ToggleTarget = { denied: ReturnType<typeof fail> } | { uid: string };

function resolveToggle(name: string, session: Session | null | undefined): ToggleTarget {
  const def = moduleDef(name);
  if (!def || def.toggleable === false || def.parent) return { denied: fail(400, { ok: false, error: 'Unknown module.' }) };
  if (!assertModuleWritable(session, def)) return { denied: fail(403, { ok: false, error: 'Not allowed.' }) };
  return { uid: effectiveId(session) };
}

// The prologue the tile toggle shares with every other module write: section
// gate, auth check, form parse. DEMO runs without a session (the action
// short-circuits before the store call).
async function actionContext({ request, locals }: { request: Request; locals: App.Locals }) {
  gateModules(locals.session);
  if (!DEMO && !locals.session) return null;
  return { session: locals.session, form: await request.formData() };
}

export const actions: Actions = {
  // Quick tile on/off: flips enabled while preserving the stored config.
  toggle: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = ctx.form;
    const name = String(f.get('name') ?? '');
    const target = resolveToggle(name, ctx.session);
    if ('denied' in target) return target.denied;
    const enabled = f.get('is_enabled') === 'on';

    if (DEMO) return { ok: true, name, enabled };

    return flipModule({ name, uid: target.uid, enabled }, ctx.session);
  }
};

// flipModule writes the enable flag, preserving the stored config. The tile
// only flips enabled: re-read the stored config and write it back untouched.
// Never rebuild it from the tile form — the page flattens every config value
// to a string for its reply inputs, which would corrupt the nested blobs some
// modules own (channel-points rewards, timers) into "[object Object]" and wipe
// them on a toggle.
async function flipModule(
  flip: { name: string; uid: string; enabled: boolean },
  session: Session | null | undefined
) {
  const { name, uid, enabled } = flip;
  try {
    const rows = await listModules(uid);
    const config = rows.find((r) => r.name === name)?.configs;
    await upsertModule(uid, name, enabled, config);
    // Nested games cannot outlive their parent: flipping loyalty off from
    // this tile must clear gamble/duel too, matching the loyalty page toggle.
    if (!enabled) await disableChildren(uid, name);
  } catch (e) {
    logger.error({ err: e }, `[modules] toggle ${name} failed`);
    return fail(400, { ok: false });
  }

  auditDashboardImpersonation(session, 'module:toggle', `${name}=${enabled}`);
  return { ok: true, name, enabled };
}
