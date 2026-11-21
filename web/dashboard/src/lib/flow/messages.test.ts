// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { FLOW_ISSUE } from '@bagel/kit/automations';
import { cardFromSummary } from './cards';
import { deleteMessage, issueMessage, saveMessage, stepOf, toggleMessage, upgradeNotes, type Translate } from './messages';
import type { SaveReply } from './replies';

const t: Translate = (key, params) => (params ? `${key} ${JSON.stringify(params)}` : key);

const nodes = [
  { id: 'a', type: 'twitch.chat.command' },
  { id: 'b', type: 'cond.user' }
];

describe('save refusals', () => {
  test.each(Object.values(FLOW_ISSUE))('flow issue %s has its own message', (code) => {
    expect(issueMessage(t, { path: 'nodes.b', code })).not.toContain('flows.issue.generic');
  });

  test('the store codes are named', () => {
    for (const code of ['uses_token', 'command_exists', 'alert_exists', 'duplicate', 'too_many', 'perm_required']) {
      expect(issueMessage(t, { path: '', code })).not.toContain('flows.issue.generic');
    }
  });

  test.each(['brand_new', 'constructor'])('unknown code %s uses a plain fallback', (code) => {
    expect(issueMessage(t, { path: 'actions[0]', code })).toBe('flows.issue.generic');
  });

  test('a code named like an object member is not looked up as one', () => {
    expect(issueMessage(t, { path: '', code: 'constructor' })).toContain('flows.issue.generic');
  });

  test('an unmappable token names the token', () => {
    expect(issueMessage(t, { path: 'nodes.b.message.bits', code: FLOW_ISSUE.unmappableToken })).toContain('{bits}');
  });

  test.each([
    { path: 'nodes.b', code: FLOW_ISSUE.userNotResolved, expected: 'b' },
    { path: 'nodes.b.type', code: 'perm_required', expected: 'b' },
    { path: 'trigger.params.name', code: 'required', expected: 'a' },
    { path: 'nodes.gone', code: FLOW_ISSUE.cycle, expected: null },
    { path: 'name', code: 'required', expected: null }
  ])('selects the step at $path for $code', ({ path, code, expected }) => {
    expect(stepOf([{ path, code }], nodes)).toBe(expected);
  });

  test.each<{ reply: Extract<SaveReply, { ok: false }>; expected: string }>([
    { reply: { ok: false, reason: 'not_found' }, expected: 'flows.saveNotFound' },
    { reply: { ok: false, reason: 'denied', error: 'Premium only while in beta.' }, expected: 'Premium only while in beta.' },
    { reply: { ok: false, reason: 'failed' }, expected: 'flows.saveFailed' },
    { reply: { ok: false, reason: 'failed', error: 'A reward with this title already exists on your channel. Pick another title.' }, expected: 'A reward with this title already exists on your channel. Pick another title.' },
    { reply: { ok: false, reason: 'invalid', issues: [{ path: 'nodes.b', code: 'cycle' }] }, expected: 'flows.issue.cycle' },
    { reply: { ok: false, reason: 'invalid', issues: [{ path: 'nodes.b.type', code: 'perm_required' }] }, expected: 'flows.issue.permRequired' }
  ])('picks a message for $expected', ({ reply, expected }) => {
    expect(saveMessage(t, reply)).toBe(expected);
  });

  test.each([
    ['stale toggle', toggleMessage(t, { ok: false, reason: 'conflict' }), 'flows.toggleStale'],
    ['plain toggle failure', toggleMessage(t, { ok: false, reason: 'failed' }), 'flows.toggleFailed'],
    ['reward toggle failure', toggleMessage(t, { ok: false, reason: 'failed', error: 'Duplicate title' }), 'Duplicate title'],
    ['reward delete failure', deleteMessage(t, { ok: false, reason: 'failed', error: 'Reconnect required' }), 'Reconnect required']
  ])('formats %s', (_name, actual, expected) => {
    expect(actual).toBe(expected);
  });

  test('upgrade notes list only what the move loses', () => {
    const command = { from: 'command' as const, uses: 0, usesTokenInReply: false, streamOnlineOnly: true, allowedUserId: '', bumpCounter: 'deaths' };
    expect(upgradeNotes(t, command)).toEqual(['flows.upgradeLive', 'flows.upgradeCounter {"counter":"deaths"}']);
    const alert = { from: 'alert' as const, alert: 'follow' as const, repeatGuardWindowHours: 72, resubsNoLongerCovered: false };
    expect(upgradeNotes(t, alert)).toEqual(['flows.upgradeRepeat {"hours":72}']);
  });
});

describe('cardFromSummary', () => {
  const ref = { layer: 'automation' as const, id: 'x1' };

  test('survives a stored preview that is not a preview', () => {
    for (const preview of [null, 'nope', '{"n":1}']) {
      expect(cardFromSummary({ ref, name: 'A', enabled: true, revision: '1', preview })).toMatchObject({ steps: 0, preview: { n: [], e: [] } });
    }
  });

  test('counts steps and protects built-in alerts from delete', () => {
    const preview = JSON.stringify({ n: [[0, 0, 4, 0], [12, 0, 4, 2]], e: [[0, 1, 0]] });
    expect(cardFromSummary({ ref, name: 'A', enabled: false, revision: '3', preview })).toMatchObject({ id: 'a-x1', steps: 2, deletable: true, enabled: false });
    expect(cardFromSummary({ ref: { layer: 'alert', alert: 'raid' }, name: 'Raid', enabled: true, revision: 'r', preview: null })).toMatchObject({ deletable: false });
    expect(cardFromSummary({ ref: { layer: 'command', name: 'hug' }, name: '!hug', enabled: true, revision: 'r', preview: null })).toMatchObject({ id: 'c-hug' });
  });
});
