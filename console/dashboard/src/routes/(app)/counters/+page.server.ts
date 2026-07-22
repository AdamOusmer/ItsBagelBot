import type { Actions, PageServerLoad } from './$types';
import type { CounterDef, CounterEntryView, CounterScope } from '@bagel/shared';
import { COUNTER_SCOPES } from '@bagel/shared';
import { listCounters, createCounter, setCounter, renameCounter, deleteCounter, counterEntries } from '$lib/server/loyalty-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { gateModulePage } from '$lib/server/module-gate';
import type { Session } from '$lib/server/session';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

function effectiveId(session: Session | null | undefined): string {
  return session?.delegate_of ?? session?.user_id ?? 'demo';
}

// Counters belong to the loyalty module even though they live on their own
// page, so they share loyalty's delegate scope (see module-gate.ts).
function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'loyalty');
}

function demoCounters(): CounterDef[] {
  return [
    { name: 'deaths', scope: 'channel', value: 137 },
    { name: 'hugs', scope: 'viewer', value: 0 },
    { name: 'redeems', scope: 'viewer_command', value: 0 }
  ];
}

// normalizeName mirrors the loyalty service: bare key, lower-cased, no "!".
function normalizeName(raw: unknown): string {
  return String(raw ?? '')
    .trim()
    .replace(/^!/, '')
    .toLowerCase()
    .slice(0, 64);
}

// The optional ?c=<name> selects one entry-scoped counter whose stored values
// (the per-viewer buckets) are loaded alongside the list.
export const load: PageServerLoad = async ({ locals, url }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  const selected = normalizeName(url.searchParams.get('c'));
  if (env.DEMO === '1') return { counters: demoCounters(), selected: '', entries: [] as CounterEntryView[] };

  try {
    const counters = await listCounters(uid);
    let entries: CounterEntryView[] = [];
    if (selected && counters.some((c) => c.name === selected && c.scope !== 'channel')) {
      try {
        entries = await counterEntries(uid, selected, 25);
      } catch {
        /* entries are decorative next to the list */
      }
    }
    return { counters, selected, entries };
  } catch {
    return { counters: [] as CounterDef[], selected: '', entries: [] as CounterEntryView[], degraded: true };
  }
};

// mutate wraps one POST action with the shared boilerplate: gate, session,
// demo short-circuit, error mapping and the impersonation audit line. run
// returns the audit detail, or null for a validation failure.
type Mutation = (uid: string, f: FormData) => Promise<string | null>;

function mutate(op: string, run: Mutation) {
  return async ({ request, locals }: Parameters<NonNullable<Actions[string]>>[0]) => {
    gate(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    if (env.DEMO === '1') return { ok: true };
    let detail: string | null;
    try {
      detail = await run(uid, f);
    } catch (e) {
      console.error(`[counters] ${op} failed:`, e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: `${op} failed` });
    }
    if (detail === null) return fail(400, { ok: false, error: 'Invalid counter.' });
    auditDashboardImpersonation(locals.session, `counters:${op}`, detail);
    return { ok: true };
  };
}

export const actions: Actions = {
  create: mutate('create', async (uid, f) => {
    const name = normalizeName(f.get('name'));
    const scope = String(f.get('scope') ?? 'channel') as CounterScope;
    if (!name || !COUNTER_SCOPES.includes(scope)) return null;
    await createCounter(uid, name, scope);
    return `${name} (${scope})`;
  }),

  // Absolute value for a channel counter; on entry scopes value 0 doubles as
  // the reset (the service deletes every stored bucket).
  set: mutate('set', async (uid, f) => {
    const name = normalizeName(f.get('name'));
    const value = Math.trunc(Number(f.get('value')));
    if (!name || !Number.isFinite(value)) return null;
    const found = await setCounter(uid, name, value);
    if (!found) throw new Error('unknown counter');
    return `${name}=${value}`;
  }),

  rename: mutate('rename', async (uid, f) => {
    const name = normalizeName(f.get('name'));
    const newName = normalizeName(f.get('new_name'));
    if (!name || !newName || newName === name) return null;
    const found = await renameCounter(uid, name, newName);
    if (!found) throw new Error('unknown counter');
    return `${name}→${newName}`;
  }),

  delete: mutate('delete', async (uid, f) => {
    const name = normalizeName(f.get('name'));
    if (!name) return null;
    await deleteCounter(uid, name);
    return name;
  })
};
