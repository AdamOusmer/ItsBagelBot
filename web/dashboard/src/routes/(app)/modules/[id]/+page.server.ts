// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { moduleDef, type ModuleDef, MOD } from '@bagel/kit';
import { listModules, listModulesForEditing, upsertModule, patchModule, type ModuleView } from '$lib/server/commands-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/kit/server/logger';
import { gateSection, moduleLocked, moduleWriteDenial } from '$lib/server/module-gate';
import { parentIsEnabled } from '$lib/server/module-parent';
import { moduleLoad } from '$lib/server/module-page';
import { attachLinkedUUID } from '$lib/server/minecraft-uuid';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { error, fail, redirect, type Cookies } from '@sveltejs/kit';
import { actionError } from '$lib/server/action-errors';
import { notSignedIn } from '$lib/server/action-context';
import { baseCookie } from '$lib/server/cookies';
import { moduleEditState } from '$lib/server/module-edit-state';

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

function valueKeys(def: ModuleDef): string[] {
  const rules = def.id === MOD.triggers ? ['rules'] : [];
  return [...def.replies.map((r) => r.messageKey), ...(def.settings ?? []).map((s) => s.key), ...rules];
}

function enableKeys(def: ModuleDef): string[] {
  return def.replies.flatMap((r) => (r.enableKey ? [r.enableKey] : []));
}

function buildConfig(def: ModuleDef, f: FormData): Record<string, string> {
  const get = (key: string) => String(f.get(`cfg.${key}`) ?? '').trim();
  const values = valueKeys(def).filter((key) => get(key)).map((key) => [key, get(key)]);
  const disabled = enableKeys(def).filter((key) => get(key) === 'off').map((key) => [key, 'off']);
  return Object.fromEntries([...values, ...disabled]);
}

// Same keys as buildConfig, so a forged patch cannot stash arbitrary keys.
function allowedConfigKeys(def: ModuleDef): Set<string> {
  return new Set([...valueKeys(def), ...enableKeys(def)]);
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

async function gatedEnabled(uid: string, def: ModuleDef, requested: boolean): Promise<boolean> {
  if (!requested || !def.parent) return requested;
  return parentIsEnabled(uid, def.parent);
}

export const actions: Actions = {
  save: async ({ request, params, locals, cookies, url }) => {
    const target = await resolveWrite(params.id, locals);
    if ('denied' in target) return target.denied;
    const { def, uid } = target;

    const f = await request.formData();
    const requested = f.get('is_enabled') === 'on';
    if (DEMO) return { ok: true, enabled: requested };

    const enabled = await gatedEnabled(uid, def, requested);
    const config = await attachLinkedUUID(def, buildConfig(def, f), locals);

    const synced = await upsertSynced(uid, def, { enabled, config });
    if (synced === null) return fail(400, { ok: false });
    if (!synced) requestFreshRead(cookies, def.id, url);

    auditDashboardImpersonation(locals.session, 'module:update', `${def.id}=${enabled}`);
    return { ok: true, enabled };
  },

  patch: async ({ request, params, locals, cookies, url }) => {
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
    const { result, freshRead } = await applyPatch(def, uid, { enabled, expectedRev, partial: await attachLinkedUUID(def, partial, locals) }, locals.session);
    if (freshRead) requestFreshRead(cookies, def.id, url);
    return result;
  }
};

async function upsertSynced(
  uid: string,
  def: ModuleDef,
  draft: { enabled: boolean; config: Record<string, string> }
): Promise<boolean | null> {
  try {
    return (await upsertModule(uid, def.id, draft.enabled, draft.config)).synced;
  } catch (e) {
    logger.error({ err: e }, `[modules] save ${def.id} failed`);
    return null;
  }
}

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
    if (res.conflict) return { result: { ok: false, conflict: true, rev: res.rev }, freshRead: true };
    auditDashboardImpersonation(session, 'module:patch', `${def.id}=${draft.enabled}`);
    return { result: { ok: true, rev: res.rev, conflict: false }, freshRead: !res.synced };
  } catch (e) {
    logger.error({ err: e }, `[modules] patch ${def.id} failed`);
    return { result: fail(400, { ok: false }), freshRead: false };
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
