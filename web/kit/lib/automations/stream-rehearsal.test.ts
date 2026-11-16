// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { defaultScenario, rehearseDefinition } from './rehearsal';
import type { Action, Definition, Params } from './types';
import { validateDefinition } from './validate';

type Text = string;
const definition = (actions: Action[]): Definition => ({ version: 1, trigger: { type: 'command', params: { name: '!hello' } }, actions });

test.each([
  { type: 'shoutout', login: '{user}', user: '', missing: 'user' },
  { type: 'timeout', login: '{user}', user: '', missing: 'user' },
  { type: 'timeout', login: '{var:absent}', user: 'Lotti', missing: 'var:absent' },
  { type: 'shoutout', login: '', user: 'Lotti', missing: '' },
  { type: 'timeout', login: ' @ ', user: 'Lotti', missing: '' }
])('$type refuses an empty expanded target $login', ({ type, login, user, missing }) => {
  const def = definition([{ type, params: { login, seconds: 60 } }]);
  const result = rehearseDefinition(def, { ...defaultScenario(def), user });
  const event = result.events.at(-1);
  expect([event?.status, event?.detail?.code, event?.detail?.missing]).toEqual(['failed', 'empty_login', missing]);
});

test.each([
  { type: 'shoutout', params: { login: '{user}' }, kind: 'effect', lines: undefined, detail: { type: 'shoutout', login: 'PrincessBarney' } },
  { type: 'announce', params: { text: 'Hi {user}!', color: 'green' }, kind: 'say', lines: ['Hi PrincessBarney!'], detail: { mode: 'announce', color: 'green' } },
  { type: 'reply', params: { text: 'Hi {user}!' }, kind: 'say', lines: ['@PrincessBarney Hi PrincessBarney!'], detail: {} },
  { type: 'timeout', params: { login: '{user}', seconds: 60, reason: 'Stop {user}' }, kind: 'effect', lines: undefined, detail: { type: 'timeout', login: 'PrincessBarney', seconds: '60', reason: 'Stop PrincessBarney' } }
] satisfies { type: Text; params: Params; kind: Text; lines?: Text[]; detail: Record<Text, Text> }[])('$type emits its rehearsal event', ({ type, params, kind, lines, detail }) => {
  const def = definition([{ type, params }]);
  def.conditions = [{ type: 'perm', params: { level: 'mod' } }];
  const result = rehearseDefinition(def, { ...defaultScenario(def), role: 'mod' });
  const event = result.events.at(-1)!;
  expect({ kind: event.kind, status: event.status, lines: event.lines }).toEqual({ kind, status: 'ok', lines });
  expect(event.detail).toMatchObject(detail);
});

test.each(['', 'primary', 'blue', 'green', 'orange', 'purple'])('announcement accepts and rehearses color %s', (color) => {
  const def = definition([{ type: 'announce', params: { text: 'Hello!', color } }]);
  expect(validateDefinition(def)).toEqual([]);
  expect(rehearseDefinition(def, defaultScenario(def)).events.at(-1)?.detail?.color).toBe(color || 'primary');
});

test.each([
  { trigger: 'command', level: 'everyone', nested: false, codes: ['perm_required'] },
  { trigger: 'command', level: 'vip', nested: false, codes: ['perm_required'] },
  { trigger: 'command', level: 'mod', nested: false, codes: [] },
  { trigger: 'command', level: 'lead_mod', nested: false, codes: [] },
  { trigger: 'command', level: 'broadcaster', nested: false, codes: [] },
  { trigger: 'command', level: 'mod', nested: true, codes: ['perm_required'] },
  { trigger: 'event', level: 'everyone', nested: false, codes: [] }
])('timeout permission: $trigger $level nested=$nested', ({ trigger, level, nested, codes }) => {
  const action: Action = { type: 'timeout', params: { login: '{user}', seconds: 60 } };
  const condition = { type: 'perm', params: { level } };
  const def = definition(nested ? [{ type: 'if', params: { condition, then: [action] } }] : [action]);
  def.trigger = trigger === 'event' ? { type: 'event', params: { kind: 'channel.raid' } } : def.trigger;
  def.conditions = nested ? [] : [condition];
  expect(validateDefinition(def).map((issue) => issue.code)).toEqual([...codes]);
});

test.each([
  { text: 'Hi!', user: 'Lotti', lines: ['@Lotti Hi!'] },
  { text: 'Hi!', user: '@Lotti', lines: ['@Lotti Hi!'] },
  { text: 'Hi!', user: '', lines: ['Hi!'] },
  { text: ' ', user: 'Lotti', lines: [] }
])('reply mirrors Go mention behavior for $user: $text', ({ text, user, lines }) => {
  const def = definition([{ type: 'reply', params: { text } }]);
  const result = rehearseDefinition(def, { ...defaultScenario(def), user });
  expect(result.events.at(-1)?.lines).toEqual(lines);
});

test('offline scenario starts offline and skips live-only lights', () => {
  const def: Definition = { version: 1, trigger: { type: 'event', params: { kind: 'stream.offline' } }, actions: [{ type: 'lights', params: {} }] };
  const scenario = defaultScenario(def);
  const result = rehearseDefinition(def, scenario);
  expect({ live: scenario.live, starts: result.starts, status: result.events.at(-1)?.status, code: result.events.at(-1)?.detail?.code }).toEqual({ live: false, starts: true, status: 'skipped', code: 'offline' });
});
