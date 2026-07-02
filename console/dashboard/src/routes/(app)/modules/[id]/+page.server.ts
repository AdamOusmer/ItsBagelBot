import type { Actions, PageServerLoad } from './$types';
import { moduleDef } from '@bagel/shared';
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

function asConfig(raw: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (raw && typeof raw === 'object') {
    for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
      out[k] = v == null ? '' : String(v);
    }
  }
  return out;
}

export type SavedModule = { enabled: boolean; config: Record<string, string> } | null;

export const load: PageServerLoad = ({ params, locals }) => {
  gateModules(locals.session);
  const def = moduleDef(params.id);
  if (!def) throw error(404, 'Unknown module');

  const uid = effectiveId(locals.session);

  // The catalog definition renders instantly; the user's saved row streams in as
  // an unawaited promise (SvelteKit streams nested promises). The old blocking
  // `await listModules(uid)` held the whole page hostage to a cold cache — up to
  // the 2s RPC timeout before ANY paint. null = no saved row / lookup failed;
  // the page keeps the catalog defaults.
  const saved: Promise<SavedModule> =
    env.DEMO === '1'
      ? Promise.resolve(null)
      : listModules(uid)
          .then((rows): SavedModule => {
            const row = rows.find((r) => r.name === def.id);
            return row ? { enabled: row.is_enabled, config: asConfig(row.configs) } : null;
          })
          .catch((): SavedModule => null);

  return {
    def,
    defaults: { enabled: def.defaultEnabled, config: {} as Record<string, string> },
    saved
  };
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
