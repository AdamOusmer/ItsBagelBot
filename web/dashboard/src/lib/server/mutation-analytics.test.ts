// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';
import { actionEvent, captureAnalytics } from '../../../test/analytics';

stubSvelteKit();
const writes: unknown[] = [];
let fails = false;
const store = await import('./commands-store');
async function write(...args: unknown[]) {
  writes.push(args);
  if (fails) throw new Error('write failed');
  return { modules: [], commands: [] };
}

const storeMock = () => ({
  ...store,
  listCommands: async () => [],
  listModules: async () => [{ name: 'quotes', is_enabled: true, configs: { keep: 'value' } }],
  upsertModule: write,
  upsertCommand: write,
  deleteCommand: write
});
mock.module('#lib/server/commands-store', storeMock);
mock.module('./commands-store', storeMock);
mock.module('#lib/server/recipe-time-left', () => ({ recipeTimeLeftNames: async () => [] }));
const services = await import('./services');
mock.module('#lib/server/services', () => ({ ...services, auditDashboardImpersonation: () => {} }));
const { setModuleEnabled } = await import('./module-blob');
const { actions } = await import('../../routes/(app)/commands/+page.server');
const recorded = captureAnalytics();
beforeEach(() => {
  writes.length = 0;
  fails = false;
});

test.each([true, false])('module enable=%s leaves counting to the repository', async (enabled) => {
  await setModuleEnabled('7', 'quotes', enabled);
  expect({ writes, counts: await recorded() }).toEqual({ writes: [['7', 'quotes', enabled, { keep: 'value' }]], counts: [] });
});

test.each([
  { action: 'save', failed: false, status: undefined },
  { action: 'delete', failed: false, status: undefined },
  { action: 'save', failed: true, status: 400 },
  { action: 'delete', failed: true, status: 400 }
])('$action failed=$failed leaves counting to the repository', async ({ action, failed, status }) => {
  fails = failed;
  const event = actionEvent({ name: 'mycustom', response: 'hello', is_active: 'on', perm: 'everyone' });
  const result = await actions[action](event as never);
  expect({ writes: writes.length, status: (result as { status?: number }).status, counts: await recorded() }).toEqual({
    writes: 1, status, counts: []
  });
});
