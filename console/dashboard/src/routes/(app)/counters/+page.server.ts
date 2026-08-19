// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { CounterDef, CounterEntryView, CounterScope } from '@bagel/shared';
import { COUNTER_SCOPES } from '@bagel/shared';
import { listCounters, createCounter, renameCounter, deleteCounter, counterEntries } from '$lib/server/loyalty-store';
import { UserError, normalizeName } from '$lib/server/counter-form';
import { runSet, runAddEntry, runDeleteEntry } from '$lib/server/counter-actions';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import { gateModulePage } from '$lib/server/module-gate';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// Counters are a catalog-defined Modules tool, so the page and every action
// derive delegate access from the same definition as its tile and route guard.
function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'counters');
}

// The optional ?c=<name> selects one entry-scoped counter whose stored values
// (the per-viewer buckets) are loaded alongside the list.
export const load: PageServerLoad = async ({ locals, url }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  const selected = normalizeName(url.searchParams.get('c'));
  if (DEMO) {
    const { demoCounters, demoEntries } = await import('$lib/server/demo-data');
    const demo = demoCounters();
    const sel = demo.some((c) => c.name === selected && c.scope !== 'channel') ? selected : '';
    return { counters: demo, selected: sel, entries: sel ? demoEntries(sel) : [] };
  }

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
    if (!DEMO && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });
    const uid = effectiveId(locals.session);

    const f = await request.formData();
    if (DEMO) return { ok: true };
    let detail: string | null;
    try {
      detail = await run(uid, f);
    } catch (e) {
      if (e instanceof UserError) return fail(400, { ok: false, error: e.message });
      logger.error({ err: e }, `[counters] ${op} failed`);
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
  // the reset. An optional target (viewer_id and/or command) writes one bucket.
  set: mutate('set', runSet),

  // Manual add of one bucket to an entry-scoped counter.
  addEntry: mutate('addEntry', runAddEntry),

  rename: mutate('rename', async (uid, f) => {
    const name = normalizeName(f.get('name'));
    const newName = normalizeName(f.get('new_name'));
    if (!name || !newName) return null;
    if (newName === name) return null;
    const found = await renameCounter(uid, name, newName);
    if (!found) throw new Error('unknown counter');
    return `${name}>${newName}`;
  }),

  delete: mutate('delete', async (uid, f) => {
    const name = normalizeName(f.get('name'));
    if (!name) return null;
    await deleteCounter(uid, name);
    return name;
  }),

  // Remove one stored bucket of an entry-scoped counter.
  deleteEntry: mutate('deleteEntry', runDeleteEntry)
};
