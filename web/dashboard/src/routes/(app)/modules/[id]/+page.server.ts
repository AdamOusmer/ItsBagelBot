// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { MOD, moduleDef, type ModuleDef } from '@bagel/kit';
import { listModules, listModulesForEditing, patchModule, upsertModule, type ModuleView } from '#lib/server/commands-store';
import { auditDashboardImpersonation } from '#lib/server/services';
import { logger } from '@bagel/kit/server/logger';
import { gateSection, moduleLocked, moduleWriteDenial } from '#lib/server/module-gate';
import { moduleLoad } from '#lib/server/module-page';
import { attachLinkedUUID } from '#lib/server/minecraft-uuid';
import { effectiveId } from '#lib/server/board';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { error, fail, redirect, type Cookies } from '@sveltejs/kit';
import { actionError } from '#lib/server/action-errors';
import { notSignedIn } from '#lib/server/action-context';
import { baseCookie } from '#lib/server/cookies';
import { enableKeys, gatedEnabled, patchModuleSettings, valueKeys } from '#lib/server/module-patch';
import { moduleEditState } from '#lib/server/module-edit-state';

const DEMO = dev && env.DEMO === '1';

const FRESH_READ_COOKIE = 'bagel_module_fresh';
const FRESH_READ_MAX_AGE_SEC = 30;

function freshReadCookie(id: string, url: URL) {
  return baseCookie(url, `/modules/${id}`);
}

// The reload after a revision conflict must read the owning service: the projection may still hold the losing revision.
function requestFreshRead(cookies: Cookies, id: string, url: URL): void {
  cookies.set(FRESH_READ_COOKIE, '1', { ...freshReadCookie(id, url), maxAge: FRESH_READ_MAX_AGE_SEC });
}

function editorRows(uid: string, id: string, cookies: Cookies, url: URL): Promise<ModuleView[]> {
  if (!cookies.get(FRESH_READ_COOKIE)) return listModules(uid);
  cookies.delete(FRESH_READ_COOKIE, freshReadCookie(id, url));
  return listModulesForEditing(uid);
}

function requiredModulesOff(def: ModuleDef, rows: ModuleView[]): string[] {
  const required = new Set((def.settings ?? []).flatMap((s) => (s.requires ? [s.requires] : [])));
  return [...required].filter((id) => rows.find((r) => r.name === id)?.is_enabled !== true);
}

export const load: PageServerLoad = async ({ params, locals, cookies, url }) => {
  gateSection(locals.session, 'modules');
  const def = moduleDef(params.id);
  if (!def || def.hidden) throw error(404, actionError(locals.locale, 'Unknown module'));
  if (def.href) throw redirect(302, def.href);

  const locked = await moduleLocked(locals, def);
  const blank = () => ({ def, locked, enabled: def.defaultEnabled, config: {} as Record<string, string>, revision: 0, requiresOff: [] as string[] });

  return moduleLoad(def.id, locals.session, {
    demo: DEMO ? async () => blank() : undefined,
    read: async (uid) => {
      const rows = await editorRows(uid, def.id, cookies, url);
      const row = rows.find((r) => r.name === def.id);
      const { config, revision } = moduleEditState(row);
      return { def, locked, enabled: row ? row.is_enabled : def.defaultEnabled, config, revision, requiresOff: requiredModulesOff(def, rows) };
    },
    blank
  });
};

function buildConfig(def: ModuleDef, f: FormData): Record<string, string> {
  const get = (key: string) => String(f.get(`cfg.${key}`) ?? '').trim();
  const values = valueKeys(def).filter((key) => get(key)).map((key) => [key, get(key)]);
  const disabled = enableKeys(def).filter((key) => get(key) === 'off').map((key) => [key, 'off']);
  return Object.fromEntries([...values, ...disabled]);
}

type WriteTarget = { denied: ReturnType<typeof fail> } | { def: ModuleDef; uid: string };

async function resolveWrite(id: string, locals: App.Locals): Promise<WriteTarget> {
  gateSection(locals.session, 'modules');
  const def = moduleDef(id);
  if (!def || def.href) return { denied: fail(404, { ok: false, error: actionError(locals.locale, 'Unknown module.') }) };
  const denied = await moduleWriteDenial(locals, def);
  if (denied) return { denied };
  if (!DEMO && !locals.session) return { denied: notSignedIn(locals.locale) };
  return { def, uid: effectiveId(locals.session) };
}

export const actions: Actions = {
  save: async ({ request, params, locals, cookies, url }) => {
    const target = await resolveWrite(params.id, locals);
    if ('denied' in target) return target.denied;
    const { def, uid } = target;

    const f = await request.formData();
    const requested = f.get('is_enabled') === 'on';
    if (DEMO) return { ok: true, enabled: requested };

    const expectedRev = formRevision(f);
    if (def.id === MOD.alerts && Number.isNaN(expectedRev)) return fail(400, { ok: false });
    const enabled = await gatedEnabled(uid, def, requested);
    const config = await attachLinkedUUID(def, buildConfig(def, f), locals);

    const result = await saveModule(uid, def, { enabled, config, expectedRev });
    if (!result) return fail(400, { ok: false });
    if (!result.synced) requestFreshRead(cookies, def.id, url);
    if (result.conflict) return fail(409, { ok: false, conflict: true });

    auditDashboardImpersonation(locals.session, 'module:update', `${def.id}=${enabled}`);
    return { ok: true, enabled };
  },

  patch: async ({ request, params, locals, cookies, url }) => {
    const target = await resolveWrite(params.id, locals);
    if ('denied' in target) return target.denied;
    const { def } = target;
    const { result, freshRead } = await patchModuleSettings(def, await request.formData(), locals, DEMO);
    if (freshRead) requestFreshRead(cookies, def.id, url);
    return result;
  }
};

function formRevision(form: FormData): number {
  const raw = form.get('expected_rev');
  if (typeof raw !== 'string' || !/^\d+$/.test(raw)) return NaN;
  const revision = Number(raw);
  return Number.isSafeInteger(revision) ? revision : NaN;
}

type ModuleDraft = { enabled: boolean; config: Record<string, string>; expectedRev: number };

async function saveModule(
  uid: string,
  def: ModuleDef,
  draft: ModuleDraft
): Promise<{ synced: boolean; conflict?: boolean } | null> {
  try {
    if (def.id === MOD.alerts) return await saveChatAlerts(uid, def, draft);
    return await upsertModule(uid, def.id, draft.enabled, draft.config);
  } catch (e) {
    logger.error({ err: e }, `[modules] save ${def.id} failed`);
    return null;
  }
}

async function saveChatAlerts(uid: string, def: ModuleDef, draft: ModuleDraft) {
  const keys = [...valueKeys(def), ...enableKeys(def)];
  return patchModule({
    userId: uid,
    name: def.id,
    isEnabled: draft.enabled,
    partial: Object.fromEntries(keys.map((key) => [key, draft.config[key] ?? ''])),
    expectedRev: draft.expectedRev
  });
}
