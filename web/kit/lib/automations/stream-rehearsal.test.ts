// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { defaultScenario, rehearseDefinition } from './rehearsal';
import type { RehearsalEvent, RehearsalScenario } from './rehearsal';
import type { Action, Definition, Params } from './types';
import { validateDefinition } from './validate';

type Text = string;
const definition = (actions: Action[]): Definition => ({ version: 1, trigger: { type: 'command', params: { name: '!hello' } }, actions });

type OutcomeCase = {
  name: Text;
  params: Params;
  scenario?: Partial<RehearsalScenario>;
  expected: Partial<Pick<RehearsalEvent, 'status' | 'lines' | 'detail'>>;
};
type ActionCase = OutcomeCase & { type: Action['type']; trigger?: Definition['trigger'] };

const targetCases: OutcomeCase[] = [
  { name: 'viewer', params: { who: 'viewer', login: 'unused' }, expected: { status: 'ok', detail: { login: 'PrincessBarney' } } },
  { name: 'viewer ID without a name', params: { who: 'viewer' }, scenario: { user: '' }, expected: { status: 'ok', detail: { login: '40000001' } } },
  { name: 'viewer missing', params: { who: 'viewer' }, scenario: { userId: '' }, expected: { status: 'failed', detail: { code: 'no_viewer' } } },
  { name: 'first mention', params: { who: 'mention', login: 'unused' }, scenario: { message: '!hello hello @One, @Two' }, expected: { status: 'ok', detail: { login: 'One' } } },
  { name: 'mention missing', params: { who: 'mention' }, scenario: { message: '!hello hello' }, expected: { status: 'failed', detail: { code: 'no_target' } } },
  { name: 'picked user', params: { who: 'user', user_id: '9007199254740993', login: 'unused' }, expected: { status: 'ok', detail: { login: '9007199254740993', user_id: '9007199254740993' } } },
  { name: 'picked user missing', params: { who: 'user', login: 'unused' }, expected: { status: 'failed', detail: { code: 'no_target' } } },
  { name: 'login with first argument', params: { who: 'login', login: '{touser}' }, expected: { status: 'ok', detail: { login: 'Lotti' } } },
  { name: 'login with sender fallback', params: { who: 'login', login: '{touser}' }, scenario: { message: '!hello', user: '@Sender' }, expected: { status: 'ok', detail: { login: 'Sender', missing: '' } } },
  { name: 'legacy login', params: { login: '{user}' }, expected: { status: 'ok', detail: { login: 'PrincessBarney' } } },
  { name: 'empty viewer token', params: { login: '{user}' }, scenario: { user: '' }, expected: { status: 'failed', detail: { code: 'empty_login', missing: 'user' } } },
  { name: 'empty variable', params: { login: '{var:absent}' }, expected: { status: 'failed', detail: { code: 'empty_login', missing: 'var:absent' } } },
  { name: 'empty login', params: { login: '' }, expected: { status: 'failed', detail: { code: 'empty_login', missing: '' } } },
  { name: 'empty @ login', params: { login: ' @ ' }, expected: { status: 'failed', detail: { code: 'empty_login', missing: '' } } }
];

const lights = { device: 'desk', sku: 'H6008' };
const liveSteps: Record<string, Params> = {
  commercial: { seconds: 60 },
  marker: { description: 'Highlight' },
  clip: { seconds: 30 },
  ad_snooze: {}
};
test.each([
  ...['shoutout', 'timeout'].flatMap((type) => targetCases.map((row) => ({ ...row, type, params: { seconds: 60, ...row.params } }))),
  { name: 'offline trigger defaults to offline', type: 'lights', params: lights, trigger: { type: 'event', params: { kind: 'stream.offline' } }, expected: { status: 'skipped', detail: { code: 'offline', ...lights } } },
  { name: 'any time', type: 'lights', params: { ...lights, any_time: true }, scenario: { live: false }, expected: { status: 'ok', detail: lights } },
  { name: 'missing device before live gate', type: 'lights', params: { sku: lights.sku }, scenario: { live: false }, expected: { status: 'failed', detail: { code: 'no_device' } } },
  { name: 'SKU expands empty', type: 'lights', params: { ...lights, sku: '{var:absent}' }, expected: { status: 'failed', detail: { code: 'no_device', sku: '', missing: 'var:absent' } } },
  { name: 'target tokens use the sender', type: 'chat', params: { text: '{touser}/{target}' }, scenario: { message: '!hello', user: '@/Sender' }, expected: { status: 'ok', lines: ['/Sender//Sender'], detail: { missing: '' } } },
  { name: 'present argument does not use the sender', type: 'chat', params: { text: 'To {touser|nobody}' }, scenario: { message: '!hello @' }, expected: { status: 'ok', lines: ['To nobody'] } },
  { name: 'chat leaves bare message unset', type: 'chat', params: { text: 'Args=[{args}] Message=[{message}]' }, expected: { status: 'ok', lines: ['Args=[@Lotti good luck] Message=[]'], detail: { missing: 'message' } } },
  { name: 'expanded title and choices', type: 'poll', params: { title: 'Hi {user}', choices: ['To {touser}', 'Everyone'] }, expected: { status: 'ok', detail: { title: 'Hi PrincessBarney', choices: 'To Lotti\nEveryone' } } },
  { name: 'expanded reason and duration', type: 'timeout', params: { login: '{user}', reason: 'Stop {user}', seconds: 60 }, expected: { status: 'ok', detail: { login: 'PrincessBarney', seconds: '60', reason: 'Stop PrincessBarney' } } },
  { name: 'transformed text', type: 'text', params: { op: 'upper', input: '{user}', save_as: 'result' }, expected: { status: 'ok', detail: { input: 'PrincessBarney', value: 'PRINCESSBARNEY' } } },
  { name: 'text length', type: 'text', params: { op: 'length', input: '{touser}', save_as: 'result' }, expected: { status: 'ok', detail: { value: '5' } } },
  { name: 'expanded variable value', type: 'var_set', params: { name: 'name', value: '{user}' }, expected: { status: 'ok', detail: { value: 'PrincessBarney' } } },
  { name: 'empty target', type: 'raid', params: { login: '{var:absent}' }, expected: { status: 'failed', detail: { code: 'empty_login' } } },
  { name: 'literal target', type: 'raid', params: { login: 'bob' }, expected: { status: 'ok', detail: { login: 'bob' } } },
  ...Object.entries(liveSteps).flatMap(([type, params]) => [
    { name: 'while offline', type, params, scenario: { live: false }, expected: { status: 'skipped', detail: { code: 'offline' } } },
    { name: 'while live', type, params, scenario: { live: true }, expected: { status: 'ok' } }
  ] satisfies ActionCase[]),
  { name: 'defers result tokens in the reply', type: 'clip', params: { reply: '{clip:title}: {clip:url}' }, expected: { status: 'ok', detail: { reply: '{clip:title}: {clip:url}', missing: '' } } },
  { name: 'cannot read its result in the title', type: 'clip', params: { title: '{clip:title}', reply: '{clip:url}' }, expected: { status: 'ok', detail: { reply: '{clip:url}', missing: 'clip:title' } } }
] satisfies ActionCase[])('$type rehearses $name', (row: ActionCase) => {
  const def = definition([{ type: row.type, params: row.params }]);
  if (row.trigger) def.trigger = row.trigger;
  const result = rehearseDefinition(def, { ...defaultScenario(def), ...row.scenario });
  expect(result.events.at(-1)).toMatchObject(row.expected);
});

test.each([
  { type: 'announce', params: { text: 'Hi {user}!', color: 'green' }, kind: 'say', lines: ['Hi PrincessBarney!'], detail: { mode: 'announce', color: 'green' } },
  { type: 'reply', params: { text: 'Hi {user}!' }, kind: 'say', lines: ['@PrincessBarney Hi PrincessBarney!'], detail: {} }
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
  { trigger: 'command', level: 'mod', nested: true, codes: [] },
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
