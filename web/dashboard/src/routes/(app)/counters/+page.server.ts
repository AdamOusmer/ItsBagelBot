// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { CounterDef, CounterEntryView, CounterScope } from '@bagel/kit';
import { COUNTER_SCOPES } from '@bagel/kit';
import { listCounters, createCounter, renameCounter, deleteCounter, counterEntries } from '$lib/server/loyalty-store';
import { UserError, normalizeCounterName } from '$lib/server/counter-form';
import { runSet, runAddEntry, runDeleteEntry } from '$lib/server/counter-actions';
import { moduleLoad } from '$lib/server/module-page';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';
import { moduleAction } from '$lib/server/module-action';

const DEMO = dev && env.DEMO === '1';

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
        }
      }
      return { counters, selected, entries };
    },
    blank: () => ({ counters: [] as CounterDef[], selected: '', entries: [] as CounterEntryView[] })
  });
};

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

  set: mutate('set', runSet),

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

  deleteEntry: mutate('deleteEntry', runDeleteEntry)
};
