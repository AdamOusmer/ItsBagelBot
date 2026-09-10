// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { CounterDef, CounterEntryView, CounterScope } from '@bagel/shared';
import { COUNTER_SCOPES } from '@bagel/shared';
import { listCounters, createCounter, renameCounter, deleteCounter, counterEntries } from '$lib/server/loyalty-store';
import { UserError, normalizeCounterName } from '$lib/server/counter-form';
import { runSet, runAddEntry, runDeleteEntry } from '$lib/server/counter-actions';
import { moduleLoad } from '$lib/server/module-page';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';
import { moduleAction } from '$lib/server/module-action';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// The optional ?c=<name> selects one entry-scoped counter whose stored values
// (the per-viewer buckets) are loaded alongside the list.
export const load: PageServerLoad = ({ locals, url }) => {
  const selected = normalizeCounterName(url.searchParams.get('c'));
  return moduleLoad('counters', locals.session, {
    demo: DEMO
      ? async () => {
          const { demoCounters, demoEntries } = await import('$lib/server/demo-data');
          const demo = demoCounters();
          const sel = demo.some((c) => c.name === selected && c.scope !== 'channel') ? selected : '';
          return { counters: demo, selected: sel, entries: sel ? demoEntries(sel) : [] };
        }
      : undefined,
    read: async (uid) => {
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
    },
    blank: () => ({ counters: [] as CounterDef[], selected: '', entries: [] as CounterEntryView[] })
  });
};

// mutate binds one POST action to the module write skeleton
// ($lib/server/module-action): gate, form, demo short-circuit, error mapping,
// audit. A UserError carries a message written for the broadcaster, so it is
// answered as this verb's own refusal; anything else is ours and reaches
// moduleAction's generic handler (logged, generic line).
type Mutation = (uid: string, f: FormData) => Promise<string | null>;

function mutate(op: string, run: Mutation) {
  return moduleAction(
    'counters',
    op,
    async (uid, f) => {
      try {
        return await run(uid, f);
      } catch (e) {
        if (e instanceof UserError) return fail(400, { ok: false, error: e.message });
        throw e;
      }
    },
    { demo: DEMO, invalid: 'Invalid counter.' }
  );
}

export const actions: Actions = {
  create: mutate('create', async (uid, f) => {
    const name = normalizeCounterName(f.get('name'));
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
    const name = normalizeCounterName(f.get('name'));
    const newName = normalizeCounterName(f.get('new_name'));
    if (!name || !newName) return null;
    if (newName === name) return null;
    const found = await renameCounter(uid, name, newName);
    if (!found) throw new Error('unknown counter');
    return `${name}>${newName}`;
  }),

  delete: mutate('delete', async (uid, f) => {
    const name = normalizeCounterName(f.get('name'));
    if (!name) return null;
    await deleteCounter(uid, name);
    return name;
  }),

  // Remove one stored bucket of an entry-scoped counter.
  deleteEntry: mutate('deleteEntry', runDeleteEntry)
};
