import type { Actions, PageServerLoad } from './$types';
import type { ModuleDef, ModuleState } from '@bagel/shared';
import { MODULE_CATALOG, moduleDef, moduleDelegateSections } from '@bagel/shared';
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

// A delegate's grid drops tiles their grant cannot open (a bespoke page whose
// delegateSections the session lacks, e.g. Channel Points without that grant) —
// such a tile would only bounce off the route guard. Owners see everything.
function openable(def: ModuleDef, session: Session | null | undefined): boolean {
  if (!session?.delegate_of || !def.href) return true;
  const secs = session.sections ?? [];
  return moduleDelegateSections(def).some((sec) => secs.includes(sec));
}

// Merge the catalog (the modules we expose) with the broadcaster's stored rows.
// Modules absent from the catalog (system, bagel, ...) are never surfaced.
function merge(rows: ModuleView[], session: Session | null | undefined): ModuleState[] {
  const byName = new Map(rows.map((r) => [r.name, r]));
  return MODULE_CATALOG.filter((def) => !def.hidden && openable(def, session)).map((def) => {
    const row = byName.get(def.id);
    return {
      def,
      enabled: row ? row.is_enabled : def.defaultEnabled,
      config: asConfig(row?.configs)
    };
  });
}

// Tiles read state for the status + quick toggle; each module's own page owns the
// reply builder and per-reply toggles.
export const load: PageServerLoad = async ({ locals }) => {
  gateModules(locals.session);
  const uid = effectiveId(locals.session);
  if (env.DEMO === '1') return { modules: merge([], locals.session) };
  try {
    return { modules: merge(await listModules(uid), locals.session) };
  } catch {
    return { modules: merge([], locals.session), degraded: true };
  }
};

export const actions: Actions = {
  // Quick tile on/off: flips enabled while preserving the stored config.
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

    if (env.DEMO === '1') return { ok: true, name, enabled };

    try {
      // The tile only flips enabled: re-read the stored config and write it back
      // untouched. Never rebuild it from the tile form — the page flattens every
      // config value to a string for its reply inputs, which would corrupt the
      // nested blobs some modules own (channel-points rewards, timers) into
      // "[object Object]" and wipe them on a toggle.
      const rows = await listModules(uid);
      const config = rows.find((r) => r.name === name)?.configs;
      await upsertModule(uid, name, enabled, config);
    } catch (e) {
      console.error(`[modules] toggle ${name} failed:`, e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false });
    }

    auditDashboardImpersonation(locals.session, 'module:toggle', `${name}=${enabled}`);
    return { ok: true, name, enabled };
  }
};
