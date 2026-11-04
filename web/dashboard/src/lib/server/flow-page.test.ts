// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import type { SaveReply } from '../flow/replies';
import { stubSvelteKit } from '../../../test/sveltekit';
import type { SaveFlowResult } from './flow-store';

stubSvelteKit();
process.env.NEW_RELIC_ENABLED = 'false';

const { lowerSaveRequest, saveReply } = await import('./flow-page');
const { attachLogins } = await import('./user-login');

const node = (id: string, type: string, config: Record<string, string | number | boolean> = {}) => ({ id, type, x: 0, y: 0, config });

const payload = (nodes: unknown[], edges: unknown[], extra: Record<string, unknown> = {}) =>
  JSON.stringify({ flow: { name: 'Hug', enabled: true, nodes, edges }, ref: null, ...extra });

function firstIssuePath(lowered: ReturnType<typeof lowerSaveRequest>): string | undefined {
  if (lowered.ok || lowered.reply.ok) return undefined;
  return lowered.reply.reason === 'invalid' ? lowered.reply.issues?.[0]?.path : undefined;
}

describe('lowerSaveRequest', () => {
  test('lowers a drawn flow to a definition with a preview', () => {
    const lowered = lowerSaveRequest(
      payload(
        [node('a', 'twitch.chat.command', { trigger: '!hug' }), node('b', 'reply.chat', { message: 'hug {user}' })],
        [{ id: 'e1', from: 'a', port: 'next', to: 'b' }]
      )
    );
    expect(lowered.ok).toBe(true);
    if (lowered.ok) {
      expect(lowered.lowered.definition.trigger.type).toBe('command');
      expect(lowered.lowered.preview).toContain('"n"');
    }
  });

  test('points a refused save at the step that cannot be saved', () => {
    const lowered = lowerSaveRequest(
      payload(
        [node('a', 'twitch.chat.command', { trigger: '!hug' }), node('b', 'cond.user', { login: 'someone' }), node('c', 'reply.chat', { message: 'hi' })],
        [
          { id: 'e1', from: 'a', port: 'next', to: 'b' },
          { id: 'e2', from: 'b', port: 'yes', to: 'c' }
        ]
      )
    );
    expect(lowered).toEqual({ ok: false, reply: { ok: false, reason: 'invalid', issues: [{ path: 'nodes.b', code: 'user_not_resolved' }] } });
  });

  test.each([
    ['a Discord post with no server', node('b', 'reply.discord', { channel: '223456789012345678', message: 'hi' }), 'nodes.b.params.guild_id'],
    ['lights with no light chosen', node('b', 'reply.lights', { mode: 'color', color: '#f5a524' }), 'nodes.b.params.device'],
    ['a comparison with no value', node('b', 'cond.compare', { left: '', op: 'gte', right: '1' }), 'nodes.b.params.left']
  ])('selects the step when validation refuses %s', (_name, step, path) => {
    const lowered = lowerSaveRequest(
      payload([node('a', 'twitch.follow'), step], [{ id: 'e1', from: 'a', port: 'next', to: 'b' }])
    );
    expect(firstIssuePath(lowered)).toBe(path);
  });

  test.each([
    ['not json', 'nope'],
    ['no flow', JSON.stringify({ ref: null })],
    ['bad node', payload([{ id: 1 }], [])],
    ['bad ref', payload([], [], { ref: { layer: 'alert', alert: 'nope' } })],
    ['too large', 'x'.repeat(300 * 1024)]
  ])('refuses %s', (_name, text) => {
    const lowered = lowerSaveRequest(text);
    expect(lowered.ok).toBe(false);
  });
});

describe('saveReply', () => {
  const issue = { path: '', code: 'x' };

  test('names the route a saved flow now lives at', () => {
    const flow = { ref: { layer: 'command' as const, name: 'hug' }, name: '!hug', enabled: true, revision: 'r1', definition: { version: 1, trigger: { type: 'command' } } };
    expect(saveReply({ ok: true, flow, warnings: ['shadowed_by_command'] })).toMatchObject({ ok: true, id: 'c-hug', revision: 'r1', warnings: ['shadowed_by_command'] });
  });

  const refusals: [SaveFlowResult, SaveReply][] = [
    [{ ok: false, reason: 'conflict', issues: [issue] }, { ok: false, reason: 'conflict', issues: [issue] }],
    [{ ok: false, reason: 'not_found' }, { ok: false, reason: 'not_found' }],
    [{ ok: false, reason: 'failed', error: 'rpc down at 10.0.0.4' }, { ok: false, reason: 'failed' }],
    [{ ok: false, reason: 'denied', error: 'no' }, { ok: false, reason: 'denied', error: 'no' }]
  ];

  test.each(refusals)('maps %o', (result, expected) => {
    expect(saveReply(result)).toEqual(expected);
  });
});

describe('attachLogins', () => {
  const user = (userId: string) => ({ type: 'cond.user', config: { userId } as Record<string, string | number | boolean> });

  test('shows the current name and keeps the id', async () => {
    const nodes = [user('42'), { type: 'reply.chat', config: {} }];
    await attachLogins(nodes, async () => 'bagel');
    expect(nodes[0]!.config).toEqual({ userId: '42', login: 'bagel' });
  });

  test('keeps the id when the lookup finds nothing', async () => {
    const nodes = [user('42')];
    await attachLogins(nodes, async () => null);
    expect(nodes[0]!.config).toEqual({ userId: '42' });
  });
});
