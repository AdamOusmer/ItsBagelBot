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
  expected: Partial<Pick<RehearsalEvent, 'kind' | 'status' | 'lines' | 'lineStyles' | 'detail'>>;
};
type ActionCase = OutcomeCase & { type: Action['type']; trigger?: Definition['trigger']; before?: Action[]; after?: Partial<RehearsalEvent>[] };

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
const followTrigger: Definition['trigger'] = { type: 'event', params: { kind: 'channel.follow' } };
const poll: Action = { type: 'poll', params: { title: 'Pick', choices: ['Red', 'Blue'], seconds: 60 } };
const prediction: Action = { type: 'prediction', params: { title: 'Pick', outcomes: ['Red', 'Blue'], seconds: 1 } };
const followingChat = { kind: 'say', atMs: 0, lines: ['Still here'] } satisfies Partial<RehearsalEvent>;
const actionCases: ActionCase[] = [
  ...['shoutout', 'timeout', 'ban', 'unban', 'mod', 'vip'].flatMap((type) => targetCases.map((row) => ({ ...row, type, params: { ...(type === 'timeout' ? { seconds: 60 } : {}), ...row.params } }))),
  { name: 'offline trigger defaults to offline', type: 'lights', params: lights, trigger: { type: 'event', params: { kind: 'stream.offline' } }, expected: { status: 'skipped', detail: { code: 'offline', ...lights } } },
  { name: 'any time', type: 'lights', params: { ...lights, any_time: true }, scenario: { live: false }, expected: { status: 'ok', detail: { ...lights, count: '1' } } },
  { name: 'missing device before live gate', type: 'lights', params: { sku: lights.sku, hold_s: 5 }, scenario: { live: false }, expected: { status: 'failed', detail: { code: 'no_device' } } },
  { name: 'SKU expands empty', type: 'lights', params: { ...lights, sku: '{var:absent}' }, expected: { status: 'failed', detail: { code: 'no_device', sku: '', missing: 'var:absent' } } },
  { name: 'target tokens use the sender', type: 'chat', params: { text: '{touser}/{target}' }, scenario: { message: '!hello', user: '@/Sender' }, expected: { status: 'ok', lines: ['/Sender//Sender'], detail: { missing: '' } } },
  { name: 'present argument does not use the sender', type: 'chat', params: { text: 'To {touser|nobody}' }, scenario: { message: '!hello @' }, expected: { status: 'ok', lines: ['To nobody'] } },
  { name: 'chat leaves bare message unset', type: 'chat', params: { text: 'Args=[{args}] Message=[{message}]' }, expected: { status: 'ok', lines: ['Args=[@Lotti good luck] Message=[]'], detail: { missing: 'message' } } },
  { name: 'expanded title and choices', type: 'poll', params: { title: 'Hi {user}', choices: ['To {touser}', 'Everyone'] }, expected: { status: 'ok', detail: { title: 'Hi PrincessBarney', choices: 'To Lotti\nEveryone' } } },
  { name: 'trimmed and nonblank choices', type: 'poll', params: { choices: [' One ', '{var:absent}', ' Two'] }, expected: { status: 'ok', detail: { choices: 'One\nTwo' } } },
  { name: 'too few choices after expansion', type: 'poll', params: { choices: [' One ', '{var:absent}'] }, expected: { status: 'failed', detail: { choices: 'One', code: 'too_few_choices' } } },
  { name: 'trimmed and nonblank outcomes', type: 'prediction', params: { outcomes: [' Red ', ' ', ' {user} '] }, expected: { status: 'ok', detail: { outcomes: 'Red\nPrincessBarney' } } },
  { name: 'too few outcomes after expansion', type: 'prediction', params: { outcomes: [' Yes ', '{var:absent}'] }, expected: { status: 'failed', detail: { outcomes: 'Yes', code: 'too_few_choices' } } },
  { name: 'newline inside an outcome stays one entry', type: 'prediction', params: { outcomes: ['Red\nBlue', '{var:absent}'] }, expected: { status: 'failed', detail: { outcomes: 'Red\nBlue', code: 'too_few_choices' } } },
  { name: 'no poll in this test', type: 'poll_end', params: { status: 'terminate' }, before: [prediction], expected: { status: 'skipped', detail: { code: 'nothing_running' } } },
  { name: 'earlier poll in this test', type: 'poll_end', params: { status: 'archive' }, before: [poll], expected: { status: 'ok' } },
  { name: 'earlier poll already ended', type: 'poll_end', params: { status: 'terminate' }, before: [poll, { type: 'poll_end', params: { status: 'terminate' } }], expected: { status: 'skipped', detail: { code: 'nothing_running' } } },
  { name: 'no prediction in this test', type: 'prediction_end', params: { status: 'cancel' }, before: [poll], expected: { status: 'skipped', detail: { code: 'nothing_running' } } },
  { name: 'lock an earlier prediction', type: 'prediction_end', params: { status: 'lock' }, before: [prediction], expected: { status: 'ok' } },
  { name: 'resolve a locked prediction', type: 'prediction_end', params: { status: 'resolve', outcome: '{choice:Blue,Red}' }, before: [prediction, { type: 'prediction_end', params: { status: 'lock' } }], expected: { status: 'ok', detail: { outcome: 'Blue' } } },
  { name: 'no prediction after failed start', type: 'prediction_end', params: { status: 'cancel' }, before: [{ type: 'prediction', params: { outcomes: ['One', '{var:absent}'] } }], expected: { status: 'skipped', detail: { code: 'nothing_running' } } },
  { name: 'normalized target and expanded message', type: 'whisper', params: { login: ' @{touser} ', text: 'Hi {user}' }, expected: { status: 'ok', detail: { login: 'Lotti', text: 'Hi PrincessBarney' } } },
  { name: 'empty expanded message', type: 'whisper', params: { login: 'lotti', text: '{var:absent}' }, expected: { status: 'failed', detail: { code: 'empty' } } },
  { name: 'target is checked first', type: 'whisper', params: { login: ' @ ', text: '' }, expected: { status: 'failed', detail: { code: 'empty_login' } } },
  { name: 'legacy slow default', type: 'chat_mode', params: { mode: 'slow', on: true, value: 0 }, expected: { status: 'ok', detail: { value: '30' } } },
  { name: 'slow minimum', type: 'chat_mode', params: { mode: 'slow', on: true, value: 2 }, expected: { status: 'ok', detail: { value: '3' } } },
  { name: 'slow maximum', type: 'chat_mode', params: { mode: 'slow', on: true, value: 150 }, expected: { status: 'ok', detail: { value: '120' } } },
  { name: 'followers in minutes', type: 'chat_mode', params: { mode: 'followers', on: true, value: 20 }, expected: { status: 'ok', detail: { value: '20' } } },
  { name: 'chat delay seconds', type: 'chat_mode', params: { mode: 'chat_delay', on: true, value: 6 }, expected: { status: 'ok', detail: { value: '6' } } },
  { name: 'delay off', type: 'chat_mode', params: { mode: 'chat_delay', on: false, value: 4 }, expected: { status: 'ok', detail: { value: '0' } } },
  { name: 'there is a trigger message', type: 'delete_message', params: {}, expected: { status: 'ok' } },
  { name: 'there is no trigger message', type: 'delete_message', trigger: followTrigger, params: {}, expected: { status: 'failed', detail: { code: 'no_message' } } },
  { name: 'reply inside the thread', type: 'reply', params: { text: 'Hi {user}', thread: true }, expected: { status: 'ok', lines: ['Hi PrincessBarney'] } },
  { name: 'thread falls back to mention', type: 'reply', trigger: followTrigger, params: { text: 'Hi', thread: true }, expected: { status: 'ok', lines: ['@PrincessBarney Hi'] } },
  { name: 'expanded reason and duration', type: 'timeout', params: { login: '{user}', reason: 'Stop {user}', seconds: 60 }, expected: { status: 'ok', detail: { login: 'PrincessBarney', seconds: '60', reason: 'Stop PrincessBarney' } } },
  { name: 'unban request event ID', type: 'unban_resolve', params: { status: 'approve', resolution: 'Appeal {event.id}' }, trigger: { type: 'event', params: { kind: 'channel.unban_request.create' } }, expected: { status: 'ok', detail: { resolution: 'Appeal unban-request-1' } } },
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
  { name: 'cannot read its result in the title', type: 'clip', params: { title: '{clip:title}', reply: '{clip:url}' }, expected: { status: 'ok', detail: { reply: '{clip:url}', missing: 'clip:title' } } },
  {
    name: 'held lights restore the distinct devices after the next step', type: 'lights',
    params: { ...lights, device_name: 'Desk', extra_lights: ['desk/H6008', 'shelf/H6008', 'shelf/H6159', 'wall/H6159'], hold_s: 5 },
    expected: { status: 'ok', detail: { count: '3', device_name: 'Desk' } },
    after: [followingChat, { kind: 'lights_restore', path: 'actions[0]', status: 'ok', atMs: 5000, detail: { count: '3', device_name: 'Desk' } }]
  },
  {
    name: 'offline live-only lights with a hold skip without a restore', type: 'lights',
    params: { ...lights, hold_s: 5 }, scenario: { live: false },
    expected: { status: 'skipped', detail: { code: 'offline', count: '1' } }, after: [followingChat]
  }
];

test.each(actionCases)('$type rehearses $name', (row) => {
  const actions = [...(row.before ?? []), { type: row.type, params: row.params }];
  if (row.after) actions.push({ type: 'chat', params: { text: followingChat.lines[0] } });
  const def = definition(actions);
  if (row.trigger) def.trigger = row.trigger;
  const result = rehearseDefinition(def, { ...defaultScenario(def), ...row.scenario });
  expect(result.events.slice(1 + (row.before?.length ?? 0))).toMatchObject([row.expected, ...(row.after ?? [])]);
});

test.each([
  { type: 'announce', params: { text: 'Hi {user}!\nWelcome!', color: 'green' }, lines: ['Hi PrincessBarney!\nWelcome!'], styles: [{ mode: 'announce', color: 'green' }] },
  { type: 'reply', params: { text: 'Hi {user}!' }, lines: ['@PrincessBarney Hi PrincessBarney!'], styles: [{ mode: 'chat' }] },
  { type: 'pin', params: { text: 'Hi {user}!\nWelcome!' }, lines: ['Hi PrincessBarney!\nWelcome!'], styles: [{ mode: 'pin' }] },
  { type: 'chat', params: { text: 'Hi\n/announce {user}\n/announceblue Blue\n/shoutout  @Lotti Good luck\n/pin Rules\nUnused' },
    lines: ['Hi', 'PrincessBarney', 'Blue', 'Good luck', 'Rules'],
    styles: [{ mode: 'chat' }, { mode: 'announce', color: 'primary' }, { mode: 'announce', color: 'blue' }, { mode: 'shoutout', target: 'Lotti' }, { mode: 'pin' }] },
  { type: 'chat', params: { text: '/announcement hi\n/announce\n/shoutout @\n/pin' }, lines: ['/announcement hi'], styles: [{ mode: 'chat' }] }
] satisfies { type: Text; params: Params; lines: Text[]; styles: RehearsalEvent['lineStyles'] }[])('$type emits its rehearsal bubbles', ({ type, params, lines, styles }) => {
  const def = definition([{ type, params }]);
  def.conditions = [{ type: 'perm', params: { level: 'mod' } }];
  const result = rehearseDefinition(def, { ...defaultScenario(def), role: 'mod' });
  const event = result.events.at(-1)!;
  expect({ kind: event.kind, status: event.status, lines: event.lines, styles: event.lineStyles }).toEqual({ kind: 'say', status: 'ok', lines, styles });
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
  expect(result.events.at(-1)?.lines).toEqual([...lines]);
});
