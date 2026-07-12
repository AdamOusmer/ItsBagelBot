import type { Actions, PageServerLoad } from './$types';
import { moduleDef, type ModuleDef } from '@bagel/shared';
import { listModules, upsertModule, patchModule } from '$lib/server/commands-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import type { Session } from '$lib/server/session';
import { env } from '$env/dynamic/private';
import { error, fail, redirect } from '@sveltejs/kit';

function effectiveId(session: Session | null | undefined): string {
  return session?.delegate_of ?? session?.user_id ?? 'demo';
}

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
  if (env.DEMO === '1') return { def, enabled: def.defaultEnabled, config: {} as Record<string, string>, revision: 0 };

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
  const rules = def.id === 'triggers' ? get('rules') : '';
  if (rules) config.rules = rules;
  return config;
}

export const actions: Actions = {
  // One save persists the whole module config (enable + every reply message and
  // per-reply toggle). The client always posts the full draft, so upsertModule's
  // config replace is authoritative.
  save: async ({ request, params, locals }) => {
    gateModules(locals.session);
    const def = moduleDef(params.id);
    if (!def) return fail(404, { ok: false, error: 'Unknown module.' });
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) {
      return fail(401, { ok: false, error: 'Not signed in.' });
    }

    const f = await request.formData();
    const enabled = f.get('is_enabled') === 'on';
    const config = buildConfig(def, f);

    if (env.DEMO === '1') return { ok: true, enabled };

    try {
      await upsertModule(uid, def.id, enabled, config);
    } catch (e) {
      console.error(`[modules] save ${def.id} failed:`, e instanceof Error ? (e.stack ?? e.message) : e);
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
    gateModules(locals.session);
    const def = moduleDef(params.id);
    if (!def) return fail(404, { ok: false, error: 'Unknown module.' });
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) {
      return fail(401, { ok: false, error: 'Not signed in.' });
    }

    const f = await request.formData();
    const enabled = f.get('is_enabled') === 'on';
    const expectedRev = Number(f.get('expected_rev') ?? '0') || 0;
    const partial = parsePartial(f.get('partial'));
    if (!partial) return fail(400, { ok: false, error: 'Invalid patch.' });

    if (env.DEMO === '1') return { ok: true, rev: expectedRev + 1, conflict: false };

    try {
      const res = await patchModule({ userId: uid, name: def.id, isEnabled: enabled, partial, expectedRev });
      if (res.conflict) return { ok: false, conflict: true, rev: res.rev };
      auditDashboardImpersonation(locals.session, 'module:patch', `${def.id}=${enabled}`);
      return { ok: true, rev: res.rev, conflict: false };
    } catch (e) {
      console.error(`[modules] patch ${def.id} failed:`, e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false });
    }
  }
};

// parsePartial coerces the posted patch JSON into a flat string map, or null when
// it is not a valid object.
function parsePartial(raw: FormDataEntryValue | null): Record<string, string> | null {
  try {
    const obj = JSON.parse(String(raw ?? '{}'));
    if (!obj || typeof obj !== 'object') return {};
    const partial: Record<string, string> = {};
    for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
      partial[k] = v == null ? '' : String(v);
    }
    return partial;
  } catch {
    return null;
  }
}
