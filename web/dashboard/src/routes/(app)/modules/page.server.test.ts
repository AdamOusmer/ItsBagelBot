// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();

const { MODULE_CATALOG, catalogIndexable } = await import('@bagel/kit/catalog');

type ModuleRow = Record<string, unknown>;
type Loaded = { degraded?: boolean; modules: { def: { id: string }; enabled: boolean; config: Record<string, string>; revision: number }[] };

const listed: string[] = [];
let listing: () => Promise<ModuleRow[]> = async () => [];

mock.module('$lib/server/commands-store', () => ({
  listModules: async (userId: string) => {
    listed.push(userId);
    return listing();
  }
}));

const { load } = await import('./+page.server');

const def = MODULE_CATALOG.find((d) => catalogIndexable(d) && !d.href)!;

async function loadModules(rows: () => Promise<ModuleRow[]>) {
  listing = rows;
  const locals = { session: { user_id: '7' }, locale: 'en', accountState: { value: { status: 'paid' } } };
  const data = (await load({ locals } as never)) as Loaded;
  return { data, module: data.modules.find((m) => m.def.id === def.id)! };
}

const rows: { name: string; row: ModuleRow; want: { config: Record<string, string>; revision: number } }[] = [
  {
    name: 'keeps the internal revision mirror out of the editable config',
    row: { name: def.id, is_enabled: true, revision: 8, configs: { __rev: 5, greeting: 'hi' } },
    want: { config: { greeting: 'hi' }, revision: 8 }
  },
  {
    name: 'falls back to the mirror revision for legacy replies',
    row: { name: def.id, is_enabled: true, configs: { __rev: 3 } },
    want: { config: {}, revision: 3 }
  }
];

describe('modules index load', () => {
  test.each(rows)('$name', async ({ row, want }) => {
    const { module } = await loadModules(async () => [row]);
    expect({ config: module.config, revision: module.revision }).toEqual(want);
    expect(listed.at(-1)).toBe('7');
  });

  test('a stored row overrides the catalog default', async () => {
    const { module } = await loadModules(async () => [{ name: def.id, is_enabled: !def.defaultEnabled }]);
    expect(module.enabled).toBe(!def.defaultEnabled);
  });

  test('an unreachable modules service degrades the page instead of failing it', async () => {
    const { data } = await loadModules(async () => {
      throw new Error('modules unavailable');
    });
    expect(data.degraded).toBe(true);
    expect(data.modules.length).toBeGreaterThan(0);
  });
});
