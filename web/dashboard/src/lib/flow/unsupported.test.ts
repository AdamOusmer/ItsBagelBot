// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, mock, test } from 'bun:test';
import { type Definition } from '@bagel/kit/automations';
import { stubSvelteKit } from '../../../test/sveltekit';
import type { StoredFlow } from '../server/flow-store';
import { legacyEditorHref } from './ref';

stubSvelteKit();
process.env.NEW_RELIC_ENABLED = 'false';

let definition: Definition;
const unexpected = () => { throw new Error('Unexpected store call'); };
mock.module('../server/flow-store', () => ({
  deleteFlow: unexpected,
  listFlowSummaries: unexpected,
  saveFlow: unexpected,
  setFlowEnabled: unexpected,
  loadFlow: async (): Promise<StoredFlow> => ({
    ref: { layer: 'automation', id: 'legacy' }, name: 'Legacy', enabled: true, revision: '1', definition
  })
}));

const { liveEditor } = await import('../server/flow-page');
const follow = { type: 'event', params: { kind: 'channel.follow' } };

test.each([
  { name: 'trigger', definition: { version: 1, trigger: { type: 'event', params: { kind: 'channel.follow', filters: { login: 'lotti' } } } }, code: 'unsupported_trigger' },
  { name: 'action', definition: { version: 1, trigger: follow, actions: [{ type: 'poll', params: { title: 'Pick', choices: ['One\nTwo', 'Three'], seconds: 60 } }] }, code: 'unsupported_action' },
  { name: 'disabled control flow', definition: { version: 1, trigger: follow, actions: [{ type: 'parallel', enabled: false, params: { branches: [] } }] }, code: 'unsupported_disabled_control_flow' }
] satisfies { name: string; definition: Definition; code: string }[])('keeps the specific conversion reason for $name', async (row) => {
  definition = row.definition;
  const locals = { session: { user_id: 'viewer' } } as App.Locals;
  const editor = await liveEditor(locals, 'a-legacy');
  expect({ flow: editor?.flow, reason: editor?.issues?.[0].code, href: editor?.ref && legacyEditorHref(editor.ref) })
    .toEqual({ flow: null, reason: row.code, href: '/automations/legacy' });
});
