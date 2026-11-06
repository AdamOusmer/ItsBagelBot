// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { FLOW_ISSUE } from '@bagel/kit/automations';
import { cardFromSummary } from './cards';
import { issueMessage, saveMessage, stepOf, toggleMessage, upgradeNotes, type Translate } from './messages';

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
    for (const code of ['uses_token', 'command_exists', 'alert_exists', 'duplicate', 'too_many']) {
      expect(issueMessage(t, { path: '', code })).not.toContain('flows.issue.generic');
    }
  });

  test('a code the editor does not know still says which one', () => {
    expect(issueMessage(t, { path: 'actions[0]', code: 'brand_new' })).toBe('flows.issue.generic {"code":"brand_new"}');
  });

  test('a code named like an object member is not looked up as one', () => {
    expect(issueMessage(t, { path: '', code: 'constructor' })).toContain('flows.issue.generic');
  });

  test('an unmappable token names the token', () => {
    expect(issueMessage(t, { path: 'nodes.b.message.bits', code: FLOW_ISSUE.unmappableToken })).toContain('{bits}');
  });

  test('selects the step the issue names, or the trigger for trigger paths', () => {
    expect(stepOf([{ path: 'nodes.b', code: FLOW_ISSUE.userNotResolved }], nodes)).toBe('b');
    expect(stepOf([{ path: 'trigger.params.name', code: 'required' }], nodes)).toBe('a');
    expect(stepOf([{ path: 'nodes.gone', code: FLOW_ISSUE.cycle }], nodes)).toBeNull();
    expect(stepOf([{ path: 'name', code: 'required' }], nodes)).toBeNull();
  });

  test('picks a message for each way a save is refused', () => {
    expect(saveMessage(t, { ok: false, reason: 'not_found' })).toBe('flows.saveNotFound');
    expect(saveMessage(t, { ok: false, reason: 'denied', error: 'Premium only while in beta.' })).toBe('Premium only while in beta.');
    expect(saveMessage(t, { ok: false, reason: 'failed' })).toBe('flows.saveFailed');
    expect(saveMessage(t, { ok: false, reason: 'invalid', issues: [{ path: 'nodes.b', code: 'cycle' }] })).toBe('flows.issue.cycle');
  });

  test('toggle refusals say what changed', () => {
    expect(toggleMessage(t, { ok: false, reason: 'conflict' })).toBe('flows.toggleStale');
    expect(toggleMessage(t, { ok: false, reason: 'failed' })).toBe('flows.toggleFailed');
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
