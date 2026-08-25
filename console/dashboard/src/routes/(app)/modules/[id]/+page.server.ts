// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { moduleDef, type ModuleDef, MOD } from '@bagel/shared';
import { listModules, upsertModule, patchModule } from '$lib/server/commands-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import { assertModuleWritable } from '$lib/server/module-gate';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { error, fail, redirect } from '@sveltejs/kit';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

function gateModules(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('modules')) {
    throw redirect(302, '/');
  }
}

// Coerce a stored module config blob into a flat string map for the reply forms,
// pulling the server's revision mirror (__rev) out into a separate number the
// client echoes back on patch. __rev never appears as a user-facing config key.
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
  if (!def || def.hidden) throw error(404, 'Unknown module');
  // href modules (channel points, timers, govee) own a bespoke page; the generic
  // reply inspector cannot render them, so send any direct hit there.
  if (def.href) throw redirect(302, def.href);

  const uid = effectiveId(locals.session);
  if (DEMO) return { def, enabled: def.defaultEnabled, config: {} as Record<string, string>, revision: 0 };

  try {
    const rows = await listModules(uid);
    const row = rows.find((r) => r.name === def.id);
    const { config, revision } = asConfig(row?.configs);
    return {
      def,
      enabled: row ? row.is_enabled : def.defaultEnabled,
      config,
      revision
    };
  } catch {
    // Surface defaults rather than a blank page if the read is momentarily down.
    return { def, enabled: def.defaultEnabled, config: {} as Record<string, string>, revision: 0, degraded: true };
  }
};

// buildConfig reads the posted draft into the module's stored config: a
// customized message per reply (blank falls back to the sesame default), an
// explicit "off" for a per-reply toggle the user turned off (empty/absent = on,
// matching sesame's alertOn semantics), and each non-blank plain setting. Only
// non-default values are stored, so the blob stays minimal.
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
  // Triggers persists its whole rule list as one "rules" string (one rule per
  // line); the sesame module parses it (app/sesame/modules/triggers.go).
  const rules = def.id === MOD.triggers ? get('rules') : '';
  if (rules) config.rules = rules;
  return config;
}

// allowedConfigKeys names every key a module's own page can legitimately
// write, mirroring buildConfig above key for key: each reply's message and
// (if it has one) enable toggle, each plain setting, and triggers' own
// "rules" blob. patch takes a client-authored JSON delta rather than a form
// buildConfig can walk field by field, so without this a delegate (or a
// forged request) could stash arbitrary keys into the stored config that no
// UI ever reads back.
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

// resolveWrite gates a write action and resolves what it writes: the module
// def and the id whose row it touches. Every rejection is returned as `denied`
// for the action to hand straight back, so both actions read as a single gate
// call instead of repeating the same four checks and drifting apart from each
// other (and from the load) the way the read and write paths already did once.
// href modules are refused here for the same reason the load redirects them:
// their bespoke page owns the write.
type WriteTarget = { denied: ReturnType<typeof fail> } | { def: ModuleDef; uid: string };

function resolveWrite(id: string, session: Session | null | undefined): WriteTarget {
  gateModules(session);
  const def = moduleDef(id);
  if (!def || def.href) return { denied: fail(404, { ok: false, error: 'Unknown module.' }) };
  // gateModules above only proves the 'modules' section; a module with its
  // own delegation grant (channel points) needs its own scope checked too.
  if (!assertModuleWritable(session, def)) return { denied: fail(403, { ok: false, error: 'Not allowed.' }) };
  if (!DEMO && !session) return { denied: fail(401, { ok: false, error: 'Not signed in.' }) };
  return { def, uid: effectiveId(session) };
}

export const actions: Actions = {
  // One save persists the whole module config (enable + every reply message and
  // per-reply toggle). The client always posts the full draft, so upsertModule's
  // config replace is authoritative.
  save: async ({ request, params, locals }) => {
    const target = resolveWrite(params.id, locals.session);
    if ('denied' in target) return target.denied;
    const { def, uid } = target;

    const f = await request.formData();
    const enabled = f.get('is_enabled') === 'on';
    const config = buildConfig(def, f);

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

  // Patch merges only the changed keys (client-authored delta) into the stored
  // config under optimistic concurrency. `partial` is a JSON object of the keys
  // to set (an explicit "" clears one); `expected_rev` is the revision the client
  // last read. A conflict means another writer moved the revision on: the client
  // reloads and retries instead of clobbering it.
  patch: async ({ request, params, locals }) => {
    const target = resolveWrite(params.id, locals.session);
    if ('denied' in target) return target.denied;
    const { def, uid } = target;

    const f = await request.formData();
    const partial = parsePartial(f.get('partial'), def);
    if (!partial) return fail(400, { ok: false, error: 'Invalid patch.' });
    const enabled = f.get('is_enabled') === 'on';
    const expectedRev = Number(f.get('expected_rev') ?? '0') || 0;

    if (DEMO) return { ok: true, rev: expectedRev + 1, conflict: false };

    return applyPatch(def, uid, { enabled, expectedRev, partial }, locals.session);
  }
};

// applyPatch performs the optimistic-concurrency write itself, so the action
// above stays a straight read of the request. A conflict is a normal outcome
// the client retries after refetching, not a failure.
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

// parsePartial coerces the posted patch JSON into a flat string map, dropping
// any key the module def does not declare (allowedConfigKeys), or null when
// the payload is not a valid object at all. The keys are the only thing that
// makes it into the stored config, so an unknown key never has anywhere to
// land, no matter what the request tries to smuggle in.
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
