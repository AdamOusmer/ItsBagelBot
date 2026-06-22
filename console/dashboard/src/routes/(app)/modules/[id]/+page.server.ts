import type { Actions, PageServerLoad } from './$types';
import type { ModuleState } from '@bagel/shared';
import { moduleDef } from '@bagel/shared';
import { listModules, upsertModule, auditDashboardImpersonation } from '$lib/server/rpc';
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

  const uid = effectiveId(locals.session);
  let enabled = def.defaultEnabled;
  let config: Record<string, string> = {};

  if (env.DEMO !== '1') {
    try {
      const row = (await listModules(uid)).find((r) => r.name === def.id);
      if (row) {
        enabled = row.is_enabled;
        config = asConfig(row.configs);
      }
    } catch {
      /* fall back to defaults */
    }
  }

  return { state: { def, enabled, config } satisfies ModuleState };
};

export const actions: Actions = {
  save: async ({ request, params, locals }) => {
    gateModules(locals.session);
    const def = moduleDef(params.id);
    if (!def) return fail(404, { ok: false, error: 'Unknown module.' });
    const uid = effectiveId(locals.session);
    if (!locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const enabled = f.get('is_enabled') === 'on';

    // Build the config blob from the catalog's declared fields only; drop blanks.
    const config: Record<string, string> = {};
    for (const field of def.fields) {
      const v = String(f.get(`cfg.${field.key}`) ?? '').trim();
      if (v) config[field.key] = v;
    }

    const { error: err } = await upsertModule(uid, def.id, enabled, config);
    if (err) return fail(400, { ok: false, error: err });

    auditDashboardImpersonation(locals.session, 'module:update', `${def.id}=${enabled}`);
    return { ok: true, enabled };
  }
};
