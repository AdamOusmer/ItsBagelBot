import type { Actions, PageServerLoad } from './$types';
import type { ModuleState } from '@bagel/shared';
import { MODULE_CATALOG, moduleDef } from '@bagel/shared';
import { listModules, upsertModule, type ModuleView } from '$lib/server/commands-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import type { Session } from '$lib/server/session';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';

function effectiveId(session: Session | null | undefined): string {
  return session?.delegate_of ?? session?.user_id ?? 'demo';
}

// A delegate needs the 'modules' section to be here; a normal login always may.
function gateModules(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('modules')) {
    throw redirect(302, '/');
  }
}

// Coerce a stored module config blob into a flat string map for the form fields.
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
// Modules absent from the catalog (system, bagel, ...) are never surfaced.
function merge(rows: ModuleView[]): ModuleState[] {
  const byName = new Map(rows.map((r) => [r.name, r]));
  return MODULE_CATALOG.map((def) => {
    const row = byName.get(def.id);
    return {
      def,
      enabled: row ? row.is_enabled : def.defaultEnabled,
      config: asConfig(row?.configs)
    };
  });
}

export const load: PageServerLoad = async ({ locals }) => {
  gateModules(locals.session);
  const uid = effectiveId(locals.session);
  if (env.DEMO === '1') return { modules: merge([]) };
  try {
    return { modules: merge(await listModules(uid)) };
  } catch {
    return { modules: merge([]), degraded: true };
  }
};

export const actions: Actions = {
  // List-level quick toggle: flips enabled while preserving the stored config.
  toggle: async ({ request, locals }) => {
    gateModules(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) {
      return fail(401, { ok: false, error: 'Not signed in.' });
    }

    const f = await request.formData();
    const name = String(f.get('name') ?? '');
    if (!moduleDef(name)) return fail(400, { ok: false, error: 'Unknown module.' });
    const enabled = f.get('is_enabled') === 'on';

    let config: Record<string, string> | undefined;
    try {
      const raw = String(f.get('config') ?? '');
      config = raw ? (JSON.parse(raw) as Record<string, string>) : undefined;
    } catch {
      config = undefined;
    }

    // DEMO: acknowledge without RPC so the optimistic flow is exercisable.
    if (env.DEMO === '1') return { ok: true, name, enabled };

    const { error } = await upsertModule(uid, name, enabled, config);
    if (error) return fail(400, { ok: false, error });

    auditDashboardImpersonation(locals.session, 'module:toggle', `${name}=${enabled}`);
    return { ok: true, name, enabled };
  }
};
