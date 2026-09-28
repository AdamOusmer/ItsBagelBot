// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { MODULE_CATALOG, catalogIndexable, moduleDef } from '../../../../../kit/lib/catalog';
import * as moduleDefs from '../../../../../kit/lib/catalog/module-def';
import { staticText } from '../../../../../kit/lib/i18n/static';

let rows: unknown[] = [];

mock.module('$app/environment', () => ({ dev: false }));
mock.module('$env/dynamic/private', () => ({ env: {} }));
mock.module('@bagel/kit', () => ({
  ...moduleDefs,
  MODULE_CATALOG,
  catalogIndexable,
  moduleDef,
  translate: (locale: 'en' | 'fr', key: string) => staticText(locale ?? 'en', key)
}));
mock.module('$lib/server/services', () => ({
  accountState: async () => ({ status: 'paid' }),
  auditDashboardImpersonation: () => {}
}));
mock.module('$lib/server/commands-store', () => ({ listModules: async () => rows }));
mock.module('$lib/server/module-blob', () => ({ setModuleEnabled: async () => null }));
mock.module('$lib/server/module-parent', () => ({ disableChildren: async () => {} }));

const actionErrors = await import('../../../lib/server/action-errors');
mock.module('$lib/server/action-errors', () => actionErrors);
const board = await import('../../../lib/server/board');
mock.module('$lib/server/board', () => board);
const editState = await import('../../../lib/server/module-edit-state');
mock.module('$lib/server/module-edit-state', () => editState);
const gate = await import('../../../lib/server/module-gate');
mock.module('$lib/server/module-gate', () => gate);

const { load } = await import('./+page.server');

const def = MODULE_CATALOG.find((d) => catalogIndexable(d) && !d.href)!;

async function loadModule(row: unknown) {
  rows = [row];
  const data = (await load({ locals: { session: { user_id: '7' }, locale: 'en' } } as never)) as {
    modules: { def: { id: string }; config: Record<string, string>; revision: number }[];
  };
  return data.modules.find((m) => m.def.id === def.id)!;
}

describe('modules index load', () => {
  test('keeps the internal revision mirror out of the editable config', async () => {
    const mod = await loadModule({ name: def.id, is_enabled: true, revision: 8, configs: { __rev: 5, greeting: 'hi' } });
    expect(mod.config).toEqual({ greeting: 'hi' });
    expect(mod.revision).toBe(8);
  });

  test('falls back to the mirror revision for legacy replies', async () => {
    const mod = await loadModule({ name: def.id, is_enabled: true, configs: { __rev: 3 } });
    expect(mod.config).toEqual({});
    expect(mod.revision).toBe(3);
  });
});
