// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, spyOn, test } from 'bun:test';
import * as automations from '@bagel/kit/automations';
import { english } from './english.testing';
import type { Flow, FlowNode } from './model';
import { prepareRehearsal, rehearseDraft, rehearseFlow, setStoredValue, type RehearsalOptions, type RehearsalStep } from './rehearse';

type Id = FlowNode['id'];
type Kind = FlowNode['type'];
const n = (id: Id, type: Kind, config: FlowNode['config'] = {}): FlowNode => ({ id, type, config, x: 0, y: 0 });
const e = (from: Id, port: Id, to: Id) => ({ id: `${from}-${port}-${to}`, from, port, to });
const flowOf = (nodes: readonly FlowNode[], edges: readonly Flow['edges'][number][]): Flow => ({ id: 'test', name: 'Test', enabled: true, nodes: [...nodes], edges: [...edges] });
const run = (flow: Flow, options: RehearsalOptions = {}) => rehearseFlow(flow, english, options);
const trigger = n('t', 'twitch.chat.command', { trigger: '!test' });
const say = n('s', 'reply.chat', { message: 'hello {user}' });
const left = n('a', 'reply.chat', { message: 'A' });
const right = n('b', 'reply.chat', { message: 'B' });
const join = n('j', 'reply.chat', { message: 'JOIN' });
const wait = n('w', 'reply.wait', { seconds: 5 });
const fork = [e('t', 'next', 'a'), e('t', 'next', 'b')];
const loop = flowOf([trigger, n('l', 'reply.loop', { times: '3' }), n('s', 'reply.chat', { message: 'Pass {local:index}' }), join], [e('t', 'next', 'l'), e('l', 'body', 's'), e('l', 'done', 'j')]);

const chatTimeline: { type: Kind; config: FlowNode['config']; text: string; secondary?: string; status: RehearsalStep['status'] }[] = [
  { type: 'reply.poll', config: { title: 'Pick', seconds: 60, choices: ' One \n{var:absent}\n{user}\n' }, text: 'Poll: Pick', secondary: 'One, PrincessBarney', status: 'ok' },
  { type: 'reply.poll', config: { title: 'Pick', seconds: 60, choices: 'One\n{var:absent}' }, text: 'Needs at least 2 choices after tokens fill in', secondary: 'One', status: 'failed' },
  { type: 'reply.poll_end', config: { status: 'archive' }, text: 'Nothing running to end in this test', status: 'skipped' },
  { type: 'reply.delete_message', config: {}, text: 'Only works on a chat command', status: 'failed' },
  { type: 'reply.chat_mode', config: { mode: 'slow', on: true, wait: 0 }, text: 'Slow mode on, 30 s', status: 'ok' }
];

test.each(chatTimeline)(
  '$type shows $text in the timeline', ({ type, config, text, secondary, status }) => {
    const step = n('s', type, config);
    const result = run(flowOf([n('t', 'twitch.follow'), step], [e('t', 'next', 's')])).steps.at(-1);
    expect({ text: result?.text, secondary: result?.secondary, status: result?.status }).toEqual({ text, secondary, status });
  }
);

test.each<{ overrides: Record<string, string> | undefined; text: string; lines: string[] }>([
  { overrides: undefined, text: 'Path 1', lines: ['A'] },
  { overrides: { 'r.path': '2' }, text: 'Path 2', lines: ['B'] },
  { overrides: { 'r.path': '3' }, text: 'Path 1', lines: ['A'] }
])('random rehearsal shows $text as a sample', ({ overrides, text, lines }) => {
  const random = n('r', 'reply.random', { paths: '2', weight1: 3, weight2: 1 });
  const flow = flowOf([trigger, random, left, right], [e('t', 'next', 'r'), e('r', 'p1', 'a'), e('r', 'p2', 'b')]);
  const steps = run(flow, { overrides }).steps;
  const choice = steps.find((step) => step.id === 'r');
  expect({ text: choice?.text, sample: choice?.sample, lines: steps.flatMap((step) => step.lines ?? []) }).toEqual({ text, sample: true, lines });
});

test.each([
  { outcome: 'ok', status: 'ok', code: undefined },
  { outcome: 'failure', status: 'failed', code: 'child_failed' }
])('recipe call rehearsal pretends $outcome', ({ outcome, status, code }) => {
  const call = n('r', 'reply.run', { automationId: 'helper', name: 'Helper' });
  const flow = flowOf([trigger, call, left], [e('t', 'next', 'r'), e('r', 'next', 'a')]);
  const steps = run(flow, { overrides: { r: outcome } }).steps;
  const effect = steps.find((step) => step.id === 'r');
  expect({ text: effect?.text, note: effect?.note, status: effect?.status, code: effect?.detail?.code, next: steps.at(-1)?.lines })
    .toEqual({ text: 'Runs Helper', note: 'Test run does not open the other recipe', status, code, next: ['A'] });
});

test('stop shows its skip row and ends the rehearsal path', () => {
  const stop = n('stop', 'reply.stop');
  const flow = flowOf([trigger, stop], [e('t', 'next', 'stop')]);
  const row = run(flow).steps.at(-1);
  expect({ id: row?.id, status: row?.status, text: row?.text })
    .toEqual({ id: 'stop', status: 'skipped', text: 'Stopped here' });
});

test.each([
  { name: 'parallel lanes', flow: flowOf([trigger, left, right], fork), expected: [['a', 0, [0], [], ['A']], ['b', 0, [1], [], ['B']]] },
  { name: 'paused parallel lane', flow: flowOf([trigger, wait, left, right], [e('t', 'next', 'w'), e('t', 'next', 'b'), e('w', 'next', 'a')]), expected: [['b', 0, [1], [], ['B']], ['a', 5000, [0], [], ['A']]] },
  { name: 'diamond continuation once after both arms', flow: flowOf([trigger, left, right, join], [...fork, e('a', 'next', 'j'), e('b', 'next', 'j')]), expected: [['a', 0, [0], [], ['A']], ['b', 0, [1], [], ['B']], ['j', 0, [], [], ['JOIN']]] },
  { name: 'every loop iteration', flow: loop, expected: [['s', 0, [], [1], ['Pass 1']], ['s', 0, [], [2], ['Pass 2']], ['s', 0, [], [3], ['Pass 3']], ['j', 0, [], [], ['JOIN']]] }
])('adapter maps $name', ({ flow, expected }) => {
  const actual = run(flow).steps.filter((step) => step.kind === 'say').map((step) => [step.id, step.atMs, step.lane, step.iteration, step.lines]);
  expect<unknown>(actual).toEqual(expected);
});

test.each([
  { name: 'missing trigger', flow: flowOf([say], []), text: 'Add a When step', node: null },
  { name: 'cycle', flow: flowOf([trigger, say], [e('t', 'next', 's'), e('s', 'next', 's')]), text: 'loop', node: 's' },
  { name: 'paused merge', flow: flowOf([trigger, wait, left, right, join], [e('t', 'next', 'w'), e('t', 'next', 'b'), e('w', 'next', 'a'), e('a', 'next', 'j'), e('b', 'next', 'j')]), text: 'A path with a Wait', node: 'j' }
])('structural error: $name', ({ flow, text, node }) => {
  const result = run(flow);
  expect([result.issue?.text.toLowerCase().includes(text.toLowerCase()), result.issue?.node, result.steps]).toEqual([true, node, []]);
});

type InputValue = string | number | boolean;

test.each<{ type: Kind; config: FlowNode['config']; field: string; initial: InputValue; expected: InputValue; text: string }>([
  { type: 'twitch.cheer', config: { minBits: 1000 }, field: 'bits', initial: 500, expected: 1000, text: '500 is below 1000 bits' },
  { type: 'twitch.raid', config: { minViewers: 100 }, field: 'viewers', initial: 42, expected: 100, text: '42 is below 100 viewers' },
  { type: 'twitch.sub', config: { variant: 'resub', minMonths: 12 }, field: 'months', initial: 6, expected: 12, text: '6 is below 12 Months' },
  { type: 'twitch.sub', config: { variant: 'resub', minStreak: 4 }, field: 'streak', initial: 2, expected: 4, text: '2 is below 4 Streak' },
  { type: 'twitch.sub_gift', config: { minGifts: 10, tier: '2000', gifter: 'anonymous' }, field: 'giftCount', initial: 3, expected: 10, text: '3 is below 10 Gifted subs' },
  { type: 'twitch.ad_break', config: { minLength: 120 }, field: 'seconds', initial: 90, expected: 120, text: '90 is below 120 seconds' },
  { type: 'twitch.ad_break', config: { startedBy: 'automatic' }, field: 'automatic', initial: false, expected: true, text: 'Automatic is false, needs true' },
  { type: 'twitch.channel_update', config: { category: 'Chess' }, field: 'category', initial: 'Music', expected: 'Chess', text: 'Category is Music, needs Chess' },
  { type: 'schedule.timer', config: { every: 900 }, field: 'live', initial: false, expected: true, text: 'Skipped while the stream is offline' },
  { type: 'twitch.chat.command', config: { trigger: '!test' }, field: 'message', initial: 'hello', expected: '!test @Lotti good luck', text: 'does not match !test' }
])('would not start: $type, then accepts the explicit repair', ({ type, config, field, initial, expected, text }) => {
  const flow = flowOf([n('t', type, config), say], [e('t', 'next', 's')]);
  const before = run(flow, { scenario: { [field]: initial } });
  const after = run(flow, { scenario: before.startPatch });
  expect([before.starts, before.blocked?.includes(text), before.startPatch, after.steps.at(-1)?.id]).toEqual([false, true, { [field]: expected }, 's']);
});

test('gift subs starts with a matching scenario and resolves the gift tokens', () => {
  const root = n('t', 'twitch.sub_gift', { minGifts: 10, tier: '2000', gifter: 'named' });
  const reply = n('s', 'reply.chat', { message: '{user} gave {count} Tier {tier} subs ({total} total)' });
  const result = run(flowOf([root, reply], [e('t', 'next', 's')]));
  expect({ starts: result.starts, lines: result.steps.at(-1)?.lines, fields: result.fields.map(({ key }) => key) }).toEqual({
    starts: true, lines: ['PrincessBarney gave 10 Tier 2000 subs (20 total)'], fields: ['user', 'userId', 'tier', 'giftCount', 'total', 'anonymous', 'live']
  });
});

test.each([
  { name: 'role', config: n('c', 'cond.role', { level: 'mod' }), patch: { role: 'mod' } },
  { name: 'viewer ID', config: n('c', 'cond.user', { userId: '42', login: 'Lotti' }), patch: { userId: '42', user: 'Lotti' } },
  { name: 'first listed viewer', config: n('c', 'cond.user', { users: '42:lotti,53:bagel' }), patch: { userId: '42', user: 'lotti' } },
  { name: 'excluded viewer', config: n('c', 'cond.user', { mode: 'except', users: '040000001:barney,43:bagel' }), patch: { userId: '', user: '' } },
  { name: 'stream state', config: n('c', 'cond.live', { state: 'offline' }), patch: { live: false } },
  { name: 'compare input', config: n('c', 'cond.compare', { left: '{user.id}', op: 'eq', right: '42' }), patch: { userId: '42' } },
  { name: 'input must be greater', config: n('c', 'cond.compare', { left: '{user.id}', op: 'gt', right: '40000001' }), patch: { userId: '40000002' } },
  { name: 'input must differ', config: n('c', 'cond.compare', { left: '{user.id}', op: 'ne', right: '40000001' }), patch: { userId: '1' } },
  { name: 'gift amount', root: n('t', 'twitch.sub_gift', { minGifts: 3 }),
    config: n('c', 'cond.compare', { left: '{event.amount}', op: 'gte', right: '8' }), patch: { giftCount: 8 } }
])('make it pass changes the $name input', ({ root = trigger, config, patch }) => {
  const flow = flowOf([root, config, say], [e('t', 'next', 'c'), e('c', 'yes', 's')]);
  const before = run(flow);
  const repair = before.steps.find((step) => step.id === 'c')?.repair;
  const after = run(flow, { scenario: repair });
  expect([before.starts, before.state, before.stoppedAt, repair, before.steps.some((step) => step.id === 's'), after.steps.at(-1)?.id])
    .toEqual([true, 'stopped', 'c', patch, false, 's']);
});

test.each([
  { name: 'chance', node: n('c', 'cond.chance', { percent: 50 }), overrides: { c: 'no' }, port: 'no' },
  { name: 'cooldown', node: n('c', 'cond.cooldown', { seconds: 30 }), overrides: { c: 'cooling' }, port: 'no' },
  { name: 'API', node: n('c', 'reply.http_get', { url: 'https://example.com' }), overrides: { c: 'failure' }, port: 'failure' },
  { name: 'category', node: n('c', 'cond.category', { games: 'Minecraft' }), overrides: { c: 'no' }, port: 'no' },
  { name: 'follow', node: n('c', 'cond.follow', { minDays: 30 }), overrides: { c: 'no' }, port: 'no' },
  { name: 'account age', node: n('c', 'cond.accountAge', { minDays: 7 }), overrides: { c: 'no' }, port: 'no' }
])('per-step pretend $name uses the compiled path', ({ node, overrides, port }) => {
  const result = run(flowOf([trigger, node], [e('t', 'next', 'c')]), { overrides });
  expect(result.steps.find((step) => step.id === 'c')?.port).toBe(port);
});

test.each([
  ['!test 42', 'yes', 'Matches: 42'],
  ['!test hello', 'no', 'Does not match: hello']
])('command pattern shows the tested text for %s', (message, status, text) => {
  const check = n('c', 'cond.pattern', { pattern: '^[0-9]+$' });
  const flow = flowOf([trigger, check, say], [e('t', 'next', 'c'), e('c', 'yes', 's')]);
  const step = run(flow, { scenario: { message } }).steps.find((entry) => entry.id === 'c');
  expect([step?.status, step?.text]).toEqual([status, text]);
});

test.each([
  { name: 'disconnected step', nodes: [trigger, say, left], edges: [e('t', 'next', 's')], expected: ['t', 's'] },
  { name: 'unset viewer check', nodes: [trigger, n('c', 'cond.user'), say], edges: [e('t', 'next', 'c'), e('c', 'yes', 's')], expected: ['t', 'c'] }
])('mid-build rehearsal tolerates $name', ({ nodes, edges, expected }) => {
  const result = run(flowOf(nodes, edges));
  expect<unknown>([result.issue, result.steps.map((step) => step.id)]).toEqual([undefined, expected]);
});

test('a disabled step is shown as skipped and rehearsal continues', () => {
  const disabled = n('s', 'reply.chat', { message: 'hidden', disabled: true });
  const result = run(flowOf([trigger, disabled, join], [e('t', 'next', 's'), e('s', 'next', 'j')]));
  const rows = result.steps.slice(1).map(({ id, kind, status, text, lines }) => ({ id, kind, status, text, lines }));
  expect(rows).toEqual([
    { id: 's', kind: 'skip', status: 'skipped', text: 'Step is off', lines: undefined },
    { id: 'j', kind: 'say', status: 'ok', text: '', lines: ['JOIN'] }
  ]);
});

test('missing values stay marked and disconnected previews stay unreachable', () => {
  const result = run(flowOf([trigger, n('s', 'reply.chat', { message: 'Bits: {bits}, value: {var:unset}' }), left], [e('t', 'next', 's')]));
  expect([result.steps.at(-1)?.missing, result.steps.some((step) => step.id === 'a')]).toEqual([['Bits', 'unset'], false]);
});

test('stored input is shared with checks and chat in the executor', () => {
  const node = n('c', 'cond.var', { name: 'score', op: 'gte', value: '10' });
  const flow = flowOf([trigger, node, n('s', 'reply.chat', { message: '{var:score}' })], [e('t', 'next', 'c'), e('c', 'yes', 's')]);
  const result = run(flow, { scenario: setStoredValue({}, node, '12') });
  expect(result.steps.at(-1)?.lines).toEqual(['12']);
  expect(setStoredValue({ live: true, user: 'Sample' }, node, '12')).toEqual({ state: { channelVars: { score: '12' } } });
});

test.each([
  { type: 'twitch.raid', scenario: { user: 'Ana', viewers: 1 }, summary: 'Ana raids with 1 viewer' },
  { type: 'twitch.raid', scenario: { user: 'Ana', viewers: 42 }, summary: 'Ana raids with 42 viewers' },
  { type: 'twitch.cheer', scenario: { user: 'Ana', bits: 1, message: '' }, summary: 'Ana cheers 1 bit' },
  { type: 'twitch.cheer', scenario: { user: 'Ana', bits: 0 }, summary: 'Ana cheers 0 bits: GG!' },
  { type: 'twitch.ad_break', scenario: { automatic: true, seconds: 120 }, summary: 'An automatic ad break starts (2m)' },
  { type: 'twitch.channel_update', scenario: { title: 'Speedrun', category: 'Minecraft' }, summary: 'Stream updated to Speedrun in Minecraft' },
  { type: 'schedule.manual', scenario: { args: 'one two' }, summary: 'You run the recipe by hand: one two' }
])('scenario summary counts in words: $summary', ({ type, scenario, summary }) => {
  expect(run(flowOf([n('t', type)], []), { scenario }).summary).toBe(summary);
});

test.each([
  { times: '1', text: 'Repeat 1 time' },
  { times: '3', text: 'Repeat 3 times' }
])('loop step reads "$text"', ({ times, text }) => {
  const flow = flowOf([trigger, n('l', 'reply.loop', { times })], [e('t', 'next', 'l')]);
  expect(run(flow).steps.find((step) => step.id === 'l')?.text).toBe(text);
});

test('loop events have unique keys for repeated canvas nodes', () => {
  const keys = run(loop).steps.map((step) => step.key);
  expect(new Set(keys).size).toBe(keys.length);
});

test.each([
  { name: 'an event cannot acquire a moderator role', trigger: n('t', 'twitch.follow'), check: n('c', 'cond.role', { level: 'mod' }) },
  { name: 'a compare cannot replace the command itself', trigger, check: n('c', 'cond.compare', { left: '{args}', op: 'eq', right: 'different' }) }
])('repairs remain truthful: $name', ({ trigger, check }) => {
  const flow = flowOf([trigger, check, say], [e('t', 'next', 'c'), e('c', 'yes', 's')]);
  const result = run(flow);
  const step = result.steps.find((step) => step.id === 'c')!;
  expect(step.repair).toBeUndefined();
});

test.each([
  { op: 'eq', right: '', missing: [], reason: 'does not match' }, { op: 'gte', right: '', missing: [], reason: 'is not at least' },
  { op: 'lt', right: '', missing: [], reason: 'is not less than' },
  { op: 'eq', right: '6', missing: [], reason: 'does not match' }, { op: 'gte', right: '10', missing: [], reason: 'is not at least' },
  { op: 'lt', right: '3', missing: [], reason: 'is not less than' }, { op: 'contains', right: '6', missing: [], reason: 'does not contain' },
  { op: 'ne', right: '5', missing: [], reason: 'is equal to' },
  { op: 'gt', right: '5', missing: [], reason: 'is not more than' },
  { op: 'lte', right: '3', missing: [], reason: 'is not at most' },
  { op: 'starts', right: '6', missing: [], reason: 'does not start with' }
])('a failed $op check explains comparing with "$right"', ({ op, right, missing, reason }) => {
  const check = n('c', 'cond.compare', { left: '5', op, right });
  const step = run(flowOf([trigger, check], [e('t', 'next', 'c')])).steps.find((entry) => entry.id === 'c');
  expect([step?.status, step?.missing, step?.note]).toEqual(['no', [...missing], `5 ${reason} ${right || 'an empty value'}.`]);
});

test.each(['twitch.chat.command', 'twitch.chat.word', 'twitch.follow', 'twitch.sub', 'twitch.sub_gift', 'twitch.cheer', 'twitch.raid', 'twitch.reward', 'twitch.stream.online'])(
  '%s explains when viewer roles are unavailable', (type) => {
    const check = n('c', 'cond.role', { level: 'mod' });
    const root = n('t', type, { trigger: '!test', word: 'hello', reward: 'reward' });
    const step = run(flowOf([root, check], [e('t', 'next', 'c')])).steps.find((entry) => entry.id === 'c');
    const note = type.startsWith('twitch.chat.') ? undefined : 'Roles are known only for chat triggers and countdowns. On this trigger everyone counts as a viewer.';
    expect([step?.status, step?.note, Boolean(step?.repair)]).toEqual(['no', note, type.startsWith('twitch.chat.')]);
  }
);

test('draft tolerance leaves the edited flow untouched', () => {
  const flow = flowOf([trigger, n('c', 'cond.user'), n('s', 'reply.chat', { message: '{bits}' }), left], [e('t', 'next', 'c'), e('c', 'no', 's')]);
  const before = structuredClone(flow);
  run(flow);
  expect(flow).toEqual(before);
});

test('a changed event trigger does not reuse the prior chat message', () => {
  const result = run(flowOf([n('t', 'twitch.follow')], []), { scenario: { message: '!old' } });
  expect(result.steps[0]?.text).not.toBe('!old');
});

test.each([
  { node: n('s', 'reply.chat', { message: '{random}' }), text: '', lines: ['1'], secondary: undefined },
  { node: n('s', 'reply.chat', { message: '{random:1-10} {choice:a;b}' }), text: '', lines: ['1 a;b'], secondary: undefined },
  { node: n('s', 'reply.math', { expression: 'random(1,9)', saveAs: 'score' }), text: '1', lines: undefined, secondary: 'random(1,9)' }
])('samples and computed math values for $node.type', ({ node, text, lines, secondary }) => {
  const step = run(flowOf([trigger, node], [e('t', 'next', 's')])).steps.at(-1);
  expect<unknown>([step?.sample, step?.text, step?.lines, step?.secondary, step?.missing]).toEqual([true, text, lines, secondary, []]);
});

test.each<{ node: FlowNode; text: string; message?: string; secondary?: string }>([
  { node: n('s', 'reply.shoutout', { target: '{touser}' }), text: 'Shoutout to Sender' },
  { node: n('s', 'reply.text', { op: 'upper', input: 'hi', saveAs: 'result' }), text: 'HI' },
  { node: n('s', 'reply.var', { do: 'set', name: 'name', scope: 'channel', value: '{user}' }), text: 'Sender' },
  { node: n('s', 'reply.var', { do: 'add', name: 'score', scope: 'channel', amount: 3 }), text: '10' },
  { node: n('s', 'reply.var', { do: 'add', name: 'unset', scope: 'channel', amount: 3 }), text: '3' },
  { node: n('s', 'reply.http_get', { url: 'https://example.com/{user}', saveAs: 'result' }), text: 'https://example.com/Sender' },
  { node: n('s', 'reply.http_post', { url: 'https://example.com/{user}', body: 'Hello {user}' }), text: 'https://example.com/Sender' },
  { node: n('s', 'reply.http_get', { url: '{args}' }), message: '!test not a url', text: 'Set up this step first', secondary: 'not a url' }
])('effect descriptions show rehearsal values for $node.type', ({ node, text, message = '!test', secondary }) => {
  const flow = flowOf([trigger, node], [e('t', 'next', 's')]);
  const step = run(flow, { scenario: { message, user: 'Sender', state: { channelVars: { score: '7' } } } }).steps.at(-1);
  expect([step?.text, step?.secondary]).toEqual([text, secondary]);
});

test.each<{ node: FlowNode; text: string; state?: NonNullable<RehearsalOptions['scenario']>['state']; status?: 'ok' | 'failed' }>([
  { node: n('s', 'reply.poll', { title: 'What should {user} play?', choices: 'Chess\nCheckers', seconds: 60 }), text: 'Poll: What should Sender play?' },
  { node: n('s', 'reply.whisper', { target: '{user}', text: 'Thanks, {user}!' }), text: 'Whisper Sender: Thanks, Sender!' },
  { node: n('s', 'reply.chat_mode', { mode: 'followers', on: false }), text: 'Followers only off' },
  { node: n('s', 'reply.countdown_start', { name: 'subathon', seconds: 3600 }), text: 'Start subathon for 1h' },
  { node: n('s', 'reply.points', { amount: 10 }), state: { points: 15 }, text: 'Give 10 points (now 25)' },
  { node: n('s', 'reply.points_set', { amount: 10 }), state: { points: 15 }, text: 'Points are now 10' },
  { node: n('s', 'reply.counter', { counter: 'wins' }), state: { counters: { Wins: 3 } }, text: 'wins is now 4' },
  { node: n('s', 'reply.countdown_add', { name: 'subathon', seconds: 300 }), state: { countdowns: { subathon: 3_600_000 } }, text: 'subathon: 1h 5m left' },
  { node: n('s', 'reply.cooldown_add', { name: 'break', seconds: 30 }), state: { cooldowns: { break: 60_000 } }, text: 'break: 1m 30s left' },
  { node: n('s', 'reply.reward_update', { reward: 'hydrate', rewardTitle: 'Hydrate', setEnabled: 'off', reset: true }), text: 'Hydrate: turned off until the stream ends' },
  { node: n('s', 'reply.reward_update', { reward: 'hydrate', rewardTitle: 'Hydrate', setEnabled: 'on', cost: 500 }), text: 'Hydrate: turned on, cost set to 500' },
  { node: n('s', 'reply.reward_update', { reward: 'hydrate', rewardTitle: 'Hydrate', paused: 'pause', cooldown: 300 }), text: 'Hydrate: paused, cooldown 5m' },
  { node: n('s', 'reply.reward_update', { reward: 'hydrate', rewardTitle: 'Hydrate', paused: 'resume', cooldown: 0 }), text: 'Hydrate: resumed, cooldown off' },
  { node: n('s', 'reply.points_spend', { amount: 100 }), state: { points: 250 }, text: 'Spend 100 points (now 150)' },
  { node: n('s', 'reply.points_spend', { amount: 100 }), state: { points: 40 }, status: 'failed', text: 'Not enough points (balance: 40)' },
  { node: n('s', 'reply.countdown_stop', { name: 'subathon' }), state: { countdowns: { subathon: 60_000 } }, text: 'Stop subathon' },
  { node: n('s', 'reply.countdown_stop', { name: 'subathon' }), status: 'failed', text: 'Timer is not running' },
  { node: n('s', 'reply.cooldown_reset', { name: 'raffle' }), text: 'Reset raffle' }
])('renders the $node.type effect', ({ node, text, state, status = 'ok' }) => {
  const gate = n('c', 'cond.role', { level: 'broadcaster' });
  const flow = flowOf([trigger, gate, node], [e('t', 'next', 'c'), e('c', 'yes', 's')]);
  const step = run(flow, { scenario: { user: 'Sender', role: 'broadcaster', state } }).steps.at(-1);
  expect([step?.id, step?.kind, step?.status, step?.text]).toEqual(['s', 'effect', status, text]);
});

test.each<{ config: FlowNode['config']; text: string }>([
  { config: { deviceName: 'Desk' }, text: 'Desk goes back' },
  { config: { deviceName: 'Desk', extraLights: 'wall/H6159,side/H6008' }, text: 'Desk and 2 more go back' },
  { config: {}, text: 'The lights go back' }
])('shows "$text" when the lights restore', ({ config, text }) => {
  const lights = n('s', 'reply.lights', { device: 'desk', sku: 'H6159', mode: 'on', holdS: 5, ...config });
  const flow = flowOf([trigger, lights], [e('t', 'next', 's')]);
  const step = run(flow, { scenario: { live: true } }).steps.at(-1);
  expect([step?.id, step?.kind, step?.atMs, step?.text]).toEqual(['s', 'lights_restore', 5000, text]);
});

test.each(['twitch.stream.online', 'twitch.stream.offline', 'countdown.finished'])('stale viewer patches cannot affect %s', (type) => {
  const result = run(flowOf([n('t', type, { name: 'timer' }), n('s', 'reply.points', { amount: 10 })], [e('t', 'next', 's')]), {
    scenario: { user: 'Old', userId: '42', message: '!old', bits: 50 }
  });
  expect([result.scenario.user, result.scenario.message, result.scenario.bits]).toEqual([undefined, undefined, undefined]);
});

test.each<{ check: FlowNode; state: NonNullable<RehearsalOptions['scenario']>['state']; port: 'yes' | 'no'; text: string }>([
  { check: n('c', 'cond.points', { op: 'gte', value: 100 }), state: { points: 100 }, port: 'yes', text: 'Viewer has at least 100 points' },
  { check: n('c', 'cond.points', { op: 'gte', value: 100 }), state: { points: 99 }, port: 'no', text: 'Viewer has at least 100 points' },
  { check: n('c', 'cond.counter', { counter: 'wins', op: 'gte', value: 1 }), state: { counters: { Wins: 1 } }, port: 'yes', text: 'wins is at least 1' },
  { check: n('c', 'cond.counter', { counter: 'wins', op: 'gte', value: 1 }), state: { counters: { wins: 0 } }, port: 'no', text: 'wins is at least 1' }
])('routes $check.type through $port from the seeded state', ({ check, state, port, text }) => {
  const flow = flowOf([trigger, check, left, right], [e('t', 'next', 'c'), e('c', 'yes', 'a'), e('c', 'no', 'b')]);
  const result = run(flow, { scenario: { state } });
  const step = result.steps.find((event) => event.id === check.id);
  expect({ port: step?.port, text: step?.text, branch: result.steps.at(-1)?.id }).toEqual({ port, text, branch: port === 'yes' ? 'a' : 'b' });
});

test('completes a redemption in a reward Test run', () => {
  const reward = n('t', 'twitch.reward', { reward: 'hydrate' });
  const redemption = n('s', 'reply.redemption', { result: 'cancel' });
  const step = run(flowOf([reward, redemption], [e('t', 'next', 's')])).steps.at(-1);
  expect({ id: step?.id, status: step?.status, text: step?.text }).toEqual({ id: 's', status: 'ok', text: 'Refund the points' });
});

const modCheck = n('c', 'cond.role', { level: 'mod' });
const timeout = n('s', 'reply.timeout', { target: '{touser}', seconds: 60 });
const blankChoice = n('s', 'reply.poll', { title: 'Play?', choices: '\n \n', seconds: 60 });
const choiceIssue = 'Add at least two poll choices.';
const permissionIssue = 'This command needs a higher user level for this step. Add a User level check before it.';
const follow = n('t', 'twitch.follow');
const afterTrigger = (root: FlowNode, step: FlowNode) => flowOf([root, step], [e(root.id, 'next', step.id)]);

test.each([
  { name: 'unban step after Follow', flow: afterTrigger(follow, n('s', 'reply.unban_resolve', { status: 'deny' })),
    ids: ['t', 's'], found: ['s', 'failed', 'This step requires an Unban request trigger.', [['trigger_mismatch', undefined]]] },
  { name: 'poll with only blank choices', flow: afterTrigger(follow, blankChoice),
    ids: ['t', 's'], found: ['s', 'failed', choiceIssue, [['required', 'choices']]] },
  { name: 'poll with only one choice', flow: afterTrigger(follow, n('s', 'reply.poll', { title: 'Play?', choices: 'Yes', seconds: 60 })),
    ids: ['t', 's'], found: ['s', 'failed', 'Add at least two poll choices.', [['too_short', 'choices']]] },
  { name: 'lights without a device', flow: afterTrigger(trigger, n('s', 'reply.lights', { mode: 'color' })),
    ids: ['t', 's'], found: ['s', 'failed', 'Light is missing.', [['required', 'device'], ['required', 'device']]] },
  { name: 'variable with a blank amount', flow: afterTrigger(trigger, n('s', 'reply.var', { do: 'add', name: 'score', scope: 'channel', amount: '' })),
    ids: ['t', 's'], found: ['s', 'failed', 'A number is outside the allowed range.', [['out_of_range', 'amount']]] },
  { name: 'total delay exceeds the save limit',
    flow: flowOf([trigger, n('w', 'reply.wait', { seconds: 40 }), n('s', 'reply.wait', { seconds: 40 })], [e('t', 'next', 'w'), e('w', 'next', 's')]),
    ids: ['t', 'w', 's'], found: ['s', 'failed', 'The waits add up to more than a recipe can pause.', [['delay_budget', 'seconds']]] },
  { name: 'permission issue follows the trigger', flow: afterTrigger(trigger, timeout),
    ids: ['t', 's'], found: ['s', 'failed', permissionIssue, [['perm_required', undefined]]] },
  { name: 'permission check satisfies Save', flow: flowOf([trigger, modCheck, timeout], [e('t', 'next', 'c'), e('c', 'yes', 's')]),
    ids: ['t', 'c'], blocked: 'a check did not pass' },
  { name: 'unreached invalid step follows the failed check and precedes the other branch',
    flow: flowOf([trigger, modCheck, blankChoice, right], [e('t', 'next', 'c'), e('c', 'yes', 's'), e('c', 'no', 'b')]),
    ids: ['t', 'c', 's', 'b'], found: ['s', 'failed', choiceIssue, [['required', 'choices']]] },
  { name: 'comparison requires a search value', flow: afterTrigger(trigger, n('s', 'cond.compare', { left: '5', op: 'contains', right: '' })),
    ids: ['t', 's'], found: ['s', 'failed', 'This value is missing.', [['required', 'right']]], blocked: 'a check did not pass' },
  { name: 'announcement requires a message', flow: afterTrigger(trigger, n('s', 'reply.announce', { text: '' })),
    ids: ['t', 's'], found: ['s', 'failed', 'Message is missing.', [['required', 'text']]] },
  { name: 'reply requires a message', flow: afterTrigger(trigger, n('s', 'reply.reply', { text: '' })),
    ids: ['t', 's'], found: ['s', 'failed', 'Message is missing.', [['required', 'text']]] },
  { name: 'reward update requires a change', flow: afterTrigger(follow, n('s', 'reply.reward_update', { reward: 'hydrate', setEnabled: 'keep', paused: 'keep', cooldown: '', reset: true })),
    ids: ['t', 's'], found: ['s', 'failed', 'Pick at least one change', [['required', 'setEnabled']]] },
  { name: 'empty command', flow: afterTrigger(n('t', 'twitch.chat.command'), say), ids: ['t'], blocked: 'Set a command name first',
    found: ['t', 'failed', 'Command is missing.', [['required', 'trigger']]] },
  { name: 'empty chat word', flow: afterTrigger(n('t', 'twitch.chat.word'), say), ids: ['t'], blocked: 'Set a command name first',
    found: ['t', 'failed', 'Word is missing.', [['required', 'word']]] },
  { name: 'recipe-wide issue has its own notice', flow: { ...afterTrigger(trigger, say), queue: 'two words' }, ids: ['t', 's'],
    notice: 'Use one simple word for this name: lowercase letters and numbers only, like points or wins.' }
])('rehearsal shows save issues for $name', ({ flow, ids, blocked, found, notice }) => {
  const view = run(flow);
  const invalid = view.steps.find((step) => step.issues);
  const issueRow = invalid && [invalid.id, invalid.status, invalid.text, invalid.issues?.map((issue) => [issue.code, issue.fieldKey])];
  expect<unknown>({ ids: view.steps.map((step) => step.id), blocked: view.blocked, found: issueRow, invalidChat: invalid?.chatLines, notice: view.definitionNotice })
    .toEqual({ ids, blocked, found, invalidChat: found ? [] : undefined, notice });
});

test('repeated failed checks verify a repair only on request', () => {
  const nodes = [trigger, n('outer', 'reply.loop', { times: '100' }), n('inner', 'reply.loop', { times: '100' }), n('c', 'cond.role', { level: 'mod' }), left, right];
  const edges = [e('t', 'next', 'outer'), e('outer', 'body', 'inner'), e('inner', 'body', 'c'), e('c', 'yes', 'a'), e('c', 'no', 'b')];
  const executor = spyOn(automations, 'rehearseDefinition');
  const result = run(flowOf(nodes, edges));
  const calls = executor.mock.calls.length;
  const step = result.steps.find((step) => step.id === 'c')!;
  const verified = result.verifyRepair?.(step.repair!, step.path);
  const requested = executor.mock.calls.length;
  executor.mockRestore();
  expect([calls, requested, verified, result.steps.filter((step) => step.id === 'c').length > 100, step.repair]).toEqual([1, 2, true, true, { role: 'mod' }]);
});

test('scenario edits reuse the compiled draft', () => {
  const compiler = spyOn(automations, 'compileFlow');
  const draft = prepareRehearsal(flowOf([trigger, say], [e('t', 'next', 's')]));
  const lines = ['First', 'Second'].map((user) => rehearseDraft(draft, english, { scenario: { user } }).steps.at(-1)?.lines);
  const calls = compiler.mock.calls.length;
  compiler.mockRestore();
  expect([calls, lines]).toEqual([1, [['hello First'], ['hello Second']]]);
});

test('make it start verifies the trigger even when a later check stops the run', () => {
  const flow = flowOf([n('t', 'twitch.cheer', { minBits: 1000 }), n('c', 'cond.compare', { left: '1', op: 'eq', right: '2' }), say], [e('t', 'next', 'c'), e('c', 'yes', 's')]);
  const view = run(flow, { scenario: { bits: 1 } });
  expect(view.startPatch && view.verifyRepair?.(view.startPatch)).toBe(true);
});

test.each(['', 'Hello'])('HTTP response %j uses plain labels for missing values', (response) => {
  const flow = flowOf([trigger, n('api', 'reply.http_get', { url: 'https://example.com', saveAs: 'joke' }), n('s', 'reply.chat', { message: 'Joke: {local:joke}' })], [e('t', 'next', 'api'), e('api', 'ok', 's')]);
  const result = run(flow, { overrides: { 'api.response': response } });
  expect([result.steps.find((step) => step.id === 'api')?.note, result.steps.at(-1)?.missing]).toEqual(response ? [undefined, []] : ['Pretend response is empty.', ['joke']]);
});
