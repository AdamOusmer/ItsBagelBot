// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit();

const { MODULE_CATALOG, catalogIndexable } = await import('@bagel/kit/catalog');
const def = MODULE_CATALOG.find((d) => catalogIndexable(d) && !d.href && !d.hidden && !d.parent)!;

const reads: string[] = [];
let conflict = false;
let synced = true;

mock.module('#lib/server/commands-store', () => ({
  listModules: async () => {
    reads.push('projection');
    return [{ name: def.id, is_enabled: true, revision: 4, configs: {} }];
  },
  listModulesForEditing: async () => {
    reads.push('service');
    return [{ name: def.id, is_enabled: true, revision: 5, configs: {} }];
  },
  upsertModule: async () => ({ modules: [], synced }),
  patchModule: async (p: { expectedRev: number }) =>
    conflict ? { rev: 5, conflict: true, synced: false } : { rev: p.expectedRev + 1, conflict: false, synced }
}));
mock.module('#lib/server/module-gate', () => ({
  gateSection: () => {},
  gateModulePage: () => {},
  moduleLocked: async () => false,
  moduleWriteDenial: async () => null
}));
mock.module('#lib/server/minecraft-uuid', () => ({ attachLinkedUUID: async (_def: unknown, cfg: unknown) => cfg }));

const { actions, load } = await import('./+page.server');

type Jar = Map<string, string>;

function cookieJar(jar: Jar) {
  return {
    get: (name: string) => jar.get(name),
    set: (name: string, value: string) => void jar.set(name, value),
    delete: (name: string) => void jar.delete(name)
  };
}

const url = new URL(`https://dashboard.itsbagelbot.com/modules/${def.id}`);
const locals = { session: { user_id: '7' }, locale: 'en' };

async function view(jar: Jar): Promise<number> {
  const data = (await load({ params: { id: def.id }, locals, cookies: cookieJar(jar), url } as never)) as { revision: number };
  return data.revision;
}

async function patch(jar: Jar) {
  const body = new FormData();
  body.set('is_enabled', 'on');
  body.set('expected_rev', '4');
  body.set('partial', '{}');
  const request = new Request(`${url}?/patch`, { method: 'POST', body });
  return actions.patch({ request, params: { id: def.id }, locals, cookies: cookieJar(jar), url } as never);
}

beforeEach(() => {
  reads.length = 0;
  conflict = false;
  synced = true;
});

describe('module editor load', () => {
  test('an ordinary view reads the projection', async () => {
    expect(await view(new Map())).toBe(4);
    expect(reads).toEqual(['projection']);
  });

  test('the reload after a conflict reads the owning service once', async () => {
    const jar: Jar = new Map();
    conflict = true;
    expect(await patch(jar)).toEqual({ ok: false, conflict: true, rev: 5 });
    expect(await view(jar)).toBe(5);
    expect(await view(jar)).toBe(4);
    expect(reads).toEqual(['service', 'projection']);
  });

  test('a saved patch does not force a service read', async () => {
    const jar: Jar = new Map();
    expect(await patch(jar)).toEqual({ ok: true, rev: 5, conflict: false });
    await view(jar);
    expect(reads).toEqual(['projection']);
  });

  test('a patch the projection did not take forces the next reload to read the service', async () => {
    const jar: Jar = new Map();
    synced = false;
    expect(await patch(jar)).toEqual({ ok: true, rev: 5, conflict: false });
    await view(jar);
    expect(reads).toEqual(['service']);
  });

  test('a save the projection did not take forces the next reload to read the service', async () => {
    const jar: Jar = new Map();
    synced = false;
    const request = new Request(`${url}?/save`, { method: 'POST', body: new FormData() });
    await actions.save({ request, params: { id: def.id }, locals, cookies: cookieJar(jar), url } as never);
    await view(jar);
    expect(reads).toEqual(['service']);
  });
});
