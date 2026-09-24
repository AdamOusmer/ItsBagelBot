// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { moduleDef, type ModuleDef, MOD } from '@bagel/kit';
import { listModules, upsertModule, patchModule } from '$lib/server/commands-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/kit/server/logger';
import { assertModuleWritable, moduleLocked } from '$lib/server/module-gate';
import { parentIsEnabled } from '$lib/server/module-parent';
import { moduleLoad } from '$lib/server/module-page';
import { attachLinkedUUID } from '$lib/server/minecraft-uuid';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { error, fail, redirect } from '@sveltejs/kit';
import { actionError } from '$lib/server/action-errors';

const DEMO = dev && env.DEMO === '1';

function gateModules(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('modules')) {
    throw redirect(302, '/');
  }
}

function asConfig(raw: unknown): { config: Record<string, string>; revision: number } {
  const config: Record<string, string> = {};
  let revision = 0;
  if (raw && typeof raw === 'object') {
    for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
      if (k === '__rev') {
        revision = Number(v) || 0;
        continue;
      }
      config[k] = v == null ? '' : String(v);
    }
  }
  return { config, revision };
}

export const load: PageServerLoad = async ({ params, locals }) => {
  gateModules(locals.session);
  const def = moduleDef(params.id);
  if (!def || def.hidden) throw error(404, actionError(locals.locale, 'Unknown module'));
  if (def.href) throw redirect(302, def.href);

  const locked = await moduleLocked(locals, def);
  const blank = () => ({ def, locked, enabled: def.defaultEnabled, config: {} as Record<string, string>, revision: 0 });

  return moduleLoad(def.id, locals.session, {
    demo: DEMO ? async () => blank() : undefined,
    read: async (uid) => {
      const rows = await listModules(uid);
      const row = rows.find((r) => r.name === def.id);
      const { config, revision } = asConfig(row?.configs);
      return { def, locked, enabled: row ? row.is_enabled : def.defaultEnabled, config, revision };
    },
    blank
  });
};

function buildConfig(def: ModuleDef, f: FormData): Record<string, string> {
  const get = (key: string) => String(f.get(`cfg.${key}`) ?? '').trim();
  const config: Record<string, string> = {};
  for (const reply of def.replies) {
    const msg = get(reply.messageKey);
    if (msg) config[reply.messageKey] = msg;
    if (reply.enableKey && get(reply.enableKey) === 'off') config[reply.enableKey] = 'off';
  }
  for (const field of (def.settings ?? []).filter((s) => get(s.key))) {
    config[field.key] = get(field.key);
  }
  const rules = def.id === MOD.triggers ? get('rules') : '';
  if (rules) config.rules = rules;
  return config;
}

// Mirrors buildConfig key for key, so a forged patch cannot stash arbitrary keys.
function allowedConfigKeys(def: ModuleDef): Set<string> {
  const keys = new Set<string>();
  for (const reply of def.replies) {
    keys.add(reply.messageKey);
    if (reply.enableKey) keys.add(reply.enableKey);
  }
  for (const field of def.settings ?? []) keys.add(field.key);
  if (def.id === MOD.triggers) keys.add('rules');
  return keys;
}

type WriteTarget = { denied: ReturnType<typeof fail> } | { def: ModuleDef; uid: string };

async function resolveWrite(id: string, locals: App.Locals): Promise<WriteTarget> {
  const session = locals.session;
  gateModules(session);
  const def = moduleDef(id);
  if (!def || def.href) return { denied: fail(404, { ok: false, error: actionError(locals.locale, 'Unknown module.') }) };
  if (!assertModuleWritable(session, def)) return { denied: fail(403, { ok: false, error: actionError(locals.locale, 'Not allowed.') }) };
  if (await moduleLocked(locals, def)) return { denied: fail(403, { ok: false, error: actionError(locals.locale, 'Premium only while in beta.') }) };
  if (!DEMO && !session) return { denied: fail(401, { ok: false, error: actionError(locals.locale, 'Not signed in.') }) };
  return { def, uid: effectiveId(session) };
}

async function gatedEnabled(uid: string, def: ModuleDef, requested: boolean): Promise<boolean> {
  if (!requested || !def.parent) return requested;
  return parentIsEnabled(uid, def.parent);
}

export const actions: Actions = {
  save: async ({ request, params, locals }) => {
    const target = await resolveWrite(params.id, locals);
    if ('denied' in target) return target.denied;
    const { def, uid } = target;

    const f = await request.formData();
    const enabled = DEMO ? f.get('is_enabled') === 'on' : await gatedEnabled(uid, def, f.get('is_enabled') === 'on');
    const config = DEMO ? buildConfig(def, f) : await attachLinkedUUID(def, buildConfig(def, f), locals);

    if (DEMO) return { ok: true, enabled };

    try {
      await upsertModule(uid, def.id, enabled, config);
    } catch (e) {
      logger.error({ err: e }, `[modules] save ${def.id} failed`);
      return fail(400, { ok: false });
    }

    auditDashboardImpersonation(locals.session, 'module:update', `${def.id}=${enabled}`);
    return { ok: true, enabled };
  },

  patch: async ({ request, params, locals }) => {
    const target = await resolveWrite(params.id, locals);
    if ('denied' in target) return target.denied;
    const { def, uid } = target;

    const f = await request.formData();
    const partial = parsePartial(f.get('partial'), def);
    if (!partial) return fail(400, { ok: false, error: actionError(locals.locale, 'Invalid patch.') });
    const requested = f.get('is_enabled') === 'on';
    const expectedRev = Number(f.get('expected_rev') ?? '0') || 0;

    if (DEMO) return { ok: true, rev: expectedRev + 1, conflict: false };

    const enabled = await gatedEnabled(uid, def, requested);
    return applyPatch(def, uid, { enabled, expectedRev, partial: await attachLinkedUUID(def, partial, locals) }, locals.session);
  }
};

async function applyPatch(
  def: ModuleDef,
  uid: string,
  draft: { enabled: boolean; expectedRev: number; partial: Record<string, string> },
  session: Session | null | undefined
) {
  try {
    const res = await patchModule({
      userId: uid,
      name: def.id,
      isEnabled: draft.enabled,
      partial: draft.partial,
      expectedRev: draft.expectedRev
    });
    if (res.conflict) return { ok: false, conflict: true, rev: res.rev };
    auditDashboardImpersonation(session, 'module:patch', `${def.id}=${draft.enabled}`);
    return { ok: true, rev: res.rev, conflict: false };
  } catch (e) {
    logger.error({ err: e }, `[modules] patch ${def.id} failed`);
    return fail(400, { ok: false });
  }
}

function parsePartial(raw: FormDataEntryValue | null, def: ModuleDef): Record<string, string> | null {
  try {
    const obj = JSON.parse(String(raw ?? '{}'));
    if (!obj || typeof obj !== 'object') return {};
    const allowed = allowedConfigKeys(def);
    const entries = Object.entries(obj as Record<string, unknown>).filter(([k]) => allowed.has(k));
    return Object.fromEntries(entries.map(([k, v]) => [k, v == null ? '' : String(v)]));
  } catch {
    return null;
  }
}
