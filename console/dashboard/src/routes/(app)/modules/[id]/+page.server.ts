import type { Actions, PageServerLoad } from './$types';
import { moduleDef, type ModuleDef } from '@bagel/shared';
import { listModules, upsertModule } from '$lib/server/commands-store';
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

// Coerce a stored module config blob into a flat string map for the reply forms.
function asConfig(raw: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (raw && typeof raw === 'object') {
    for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
      out[k] = v == null ? '' : String(v);
    }
  }
  return out;
}

export const load: PageServerLoad = async ({ params, locals }) => {
  gateModules(locals.session);
  const def = moduleDef(params.id);
  if (!def) throw error(404, 'Unknown module');
  // href modules (channel points, timers, govee) own a bespoke page; the generic
  // reply inspector cannot render them, so send any direct hit there.
  if (def.href) throw redirect(302, def.href);

  const uid = effectiveId(locals.session);
  if (env.DEMO === '1') return { def, enabled: def.defaultEnabled, config: {} as Record<string, string> };

  try {
    const rows = await listModules(uid);
    const row = rows.find((r) => r.name === def.id);
    return {
      def,
      enabled: row ? row.is_enabled : def.defaultEnabled,
      config: asConfig(row?.configs)
    };
  } catch {
    // Surface defaults rather than a blank page if the read is momentarily down.
    return { def, enabled: def.defaultEnabled, config: {} as Record<string, string>, degraded: true };
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
  }
};
