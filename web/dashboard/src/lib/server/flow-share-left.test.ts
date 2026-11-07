// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { FLOW_FILE_FORMAT } from '@bagel/kit/automations';
import { installAutomationsRpcFake } from '../../../test/automations-rpc';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

mock.module('@bagel/kit/server/rate-limit', () => ({
  ValkeyRateLimiter: class {
    async check() {
      return { allowed: true, retryAfterSec: 1 };
    }
  }
}));

const manual = { version: 1, trigger: { type: 'manual', params: {} }, actions: [{ type: 'chat', params: { text: 'hi' } }] };
const withLeft = (left: { left_command: string }) => ({ ...manual, conditions: [{ type: 'cooldown', params: { user_s: 172800, ...left } }] });
const row = { enabled: true, revision: '1' };

mock.module('./flow-store', () => ({
  listFlows: async () => [
    { ...row, ref: { layer: 'automation', id: 'a4' }, name: 'Waits', definition: withLeft({ left_command: '!Wait' }) },
    { ...row, ref: { layer: 'automation', id: 'a5' }, name: 'Pattern', definition: { ...manual, trigger: { type: 'command', params: { name: 'free', match: 'regex' } } } }
  ],
  takenAlerts: async () => [],
  saveFlow: async () => ({ ok: true })
}));
mock.module('./commands-store', () => ({ listCommands: async () => [] }));

await installAutomationsRpcFake();
const { flowShareActions } = await import('./flow-share');

test("an existing recipe's time left command is taken, and its regex trigger claims no name", async () => {
  const form = new FormData();
  const flows = [
    { ref: 'f1', name: 'New', definition: withLeft({ left_command: 'wait' }) },
    { ref: 'f2', name: 'Open', definition: withLeft({ left_command: 'free' }) }
  ];
  form.set('file', new File([JSON.stringify({ format: FLOW_FILE_FORMAT, version: 1, flows })], 'recipes.json', { type: 'application/json' }));
  const actions = flowShareActions({ flowPreview: () => ({}) });
  const request = new Request('https://dashboard.itsbagelbot.com/flows', { method: 'POST', body: form });
  const locals = { session: { user_id: '7' }, locale: 'en', accountState: { value: { status: 'paid' } } };

  const result = (await actions.flowImportPreview({ request, locals } as never)) as { entries: { status: string }[] };

  expect(result.entries.map((entry) => entry.status)).toEqual(['conflict', 'ready']);
});
