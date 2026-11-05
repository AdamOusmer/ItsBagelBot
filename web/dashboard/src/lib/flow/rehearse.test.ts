// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { SETUP_REWARD_ID, SETUP_TEXT, SETUP_URL } from '@bagel/kit/automations';
import { describe as describeNode } from './describe';
import { NODE_SPECS, type Flow } from './model';
import { english } from './english.testing';
import { rehearseFlow, type RehearsalOptions, type RehearsalStep } from './rehearse';

const COMPARE_CASES = join(import.meta.dir, '../../../../../internal/domain/automation/testdata/compare_cases.json');

const n = (id: string, type: string, config: Flow['nodes'][number]['config'] = {}) => ({ id, type, x: 0, y: 0, config });
const e = (id: string, from: string, port: string, to: string) => ({ id, from, port, to });
const flowOf = (nodes: Flow['nodes'], edges: Flow['edges']): Flow => ({ id: 'x', name: 'x', enabled: true, nodes, edges });

const hug = flowOf(
  [
    n('a1', 'twitch.chat.command', { trigger: '!hug' }),
    n('a2', 'cond.chance', { percent: 85 }),
    n('a3', 'reply.chat', { message: '{user} gives {touser} a big warm hug.' }),
    n('a4', 'reply.chat', { message: 'The hug slipped away, {user}. Try again!' })
  ],
  [e('ae1', 'a1', 'next', 'a2'), e('ae2', 'a2', 'yes', 'a3'), e('ae3', 'a2', 'no', 'a4')]
);

const follow = flowOf(
  [n('f1', 'twitch.follow'), n('f2', 'reply.chat', { message: 'Welcome to the stream, {user}!' }), n('f3', 'reply.points', { amount: 100 })],
  [e('fe1', 'f1', 'next', 'f2'), e('fe2', 'f1', 'next', 'f3')]
);

const sub = flowOf(
  [
    n('s1', 'twitch.sub', { variant: 'any' }),
    n('s2', 'cond.compare', { left: '{months}', op: 'gte', right: '12' }),
    n('s3', 'reply.chat', { message: '{user} has been here {months} months. Legend!' }),
    n('s4', 'reply.chat', { message: 'Thanks for the sub, {user}!' })
  ],
  [e('se1', 's1', 'next', 's2'), e('se2', 's2', 'yes', 's3'), e('se3', 's2', 'no', 's4')]
);

const textOf = (segments: { text: string }[]) => segments.map((s) => s.text).join('');

function said(step: RehearsalStep): string {
  if (step.kind === 'viewer') return step.text;
  if (step.kind === 'bot') return step.lines.map((l) => textOf(l.segments)).join(' / ');
  return textOf(step.segments);
}

const rehearse = (flow: Flow, opts: RehearsalOptions = {}) => rehearseFlow(flow, english, opts);

const transcript = (flow: Flow, opts: RehearsalOptions = {}) => rehearse(flow, opts).map(said);

describe('rehearseFlow', () => {
  test('a command shows the viewer line, then the passing branch', () => {
    expect(transcript(hug)).toEqual([
      '!hug Lotti good luck',
      'Passes 85% of the time',
      'PrincessBarney gives Lotti a big warm hug.'
    ]);
  });

  test('a decision flips the branch', () => {
    const [, check, reply] = rehearse(hug, { decisions: { a2: 'no' } });
    expect(check.kind === 'check' && check.taken).toBe('no');
    expect(said(reply)).toBe('The hug slipped away, PrincessBarney. Try again!');
  });

  test('each step names the wire it came through, on the taken branch', () => {
    expect(rehearse(hug, { decisions: { a2: 'no' } }).map((s) => s.via)).toEqual([undefined, 'ae1', 'ae3']);
  });

  test('a compare evaluates against the trigger samples', () => {
    expect(transcript(sub)).toEqual([
      'PrincessBarney subscribed at tier 1',
      '14 is at least 12',
      'PrincessBarney has been here 14 months. Legend!'
    ]);
  });

  test('a compare takes the branch the engine takes for every shared case', () => {
    const cases: { left: string; op: string; right: string; want: boolean }[] = JSON.parse(readFileSync(COMPARE_CASES, 'utf8'));
    for (const { left, op, right, want } of cases) {
      const drawn = flowOf([n('t', 'twitch.follow'), n('c', 'cond.compare', { left, op, right })], [e('e', 't', 'next', 'c')]);
      const check = rehearse(drawn).find((step) => step.id === 'c');
      expect([JSON.stringify({ left, op, right }), check?.kind === 'check' && check.taken]).toEqual([JSON.stringify({ left, op, right }), want ? 'yes' : 'no']);
    }
  });

  test('fan-out runs every step in order, effects included', () => {
    expect(transcript(follow)).toEqual(['PrincessBarney followed the channel', 'Welcome to the stream, PrincessBarney!', 'Give 100 points']);
  });

  test('a trigger floor raises the sample so the trigger would really fire', () => {
    const raid = flowOf([n('r1', 'twitch.raid', { minViewers: 500 })], []);
    expect(said(rehearse(raid)[0])).toBe('PrincessBarney raided with 500 viewers');
  });

  test('until walks the branch that reaches the node, then stops', () => {
    const steps = rehearse(hug, { until: 'a4' });
    expect(steps.map((s) => s.id)).toEqual(['a1', 'a2', 'a4']);
    expect(said(steps[2])).toContain('slipped away');
  });

  test('an unconnected message stands alone, trigger tokens marked unknown', () => {
    const loose = flowOf([n('c1', 'twitch.cheer'), n('c2', 'reply.chat', { message: 'Thanks for {bits} bits' })], []);
    const [step] = rehearse(loose, { until: 'c2' });
    expect(step.kind === 'bot' && step.lines[0].segments.some((s) => s.kind === 'unknown' && s.text === '{bits}')).toBe(true);
  });

  test('a loop ends', () => {
    const looped = { ...follow, edges: [...follow.edges, e('back', 'f2', 'next', 'f2')] };
    expect(rehearse(looped).length).toBe(3);
  });

  test('no trigger means nothing to rehearse', () => {
    expect(rehearse(flowOf([], []))).toEqual([]);
  });

  test('a chat word trigger rehearses as the viewer saying it', () => {
    const [step] = rehearse(flowOf([n('w', 'twitch.chat.word', { word: 'bagel' })], []), { until: 'w' });
    expect(step.kind === 'viewer' && step.text).toBe('bagel');
  });

  test('a loop walks its body and then what comes after it', () => {
    const flow = flowOf(
      [n('t', 'twitch.follow'), n('l', 'reply.loop', { times: '3' }), n('b', 'reply.chat', { message: 'time {local:index}' }), n('a', 'reply.chat', { message: 'after' })],
      [e('1', 't', 'next', 'l'), e('2', 'l', 'body', 'b'), e('3', 'l', 'done', 'a')]
    );
    const steps = rehearse(flow);
    expect(steps.map((step) => step.id)).toEqual(['t', 'l', 'b', 'a']);
    const body = steps[2]!;
    expect(body.kind === 'bot' && body.lines[0]!.segments.map((seg) => seg.text).join('')).toBe('time 1');
  });

  test('an API node asks which path to rehearse and only walks that one', () => {
    const flow = flowOf(
      [n('t', 'twitch.follow'), n('g', 'reply.http_get', { url: 'https://api.example.com' }), n('w', 'reply.chat', { message: 'worked' }), n('f', 'reply.chat', { message: 'failed {local:error}' })],
      [e('1', 't', 'next', 'g'), e('2', 'g', 'ok', 'w'), e('3', 'g', 'failure', 'f')]
    );
    expect(rehearse(flow).map((step) => step.id)).toEqual(['t', 'g', 'w']);
    const failed = rehearse(flow, { decisions: { g: 'failure' } });
    expect(failed.map((step) => step.id)).toEqual(['t', 'g', 'f']);
    const last = failed[2]!;
    expect(last.kind === 'bot' && last.lines[0]!.segments.map((seg) => seg.text).join('')).toBe('failed 503');
  });

  test('an imported step awaiting setup never shows its placeholder', () => {
    const flow = flowOf(
      [n('t', 'twitch.reward', { reward: SETUP_REWARD_ID }), n('c', 'reply.chat', { message: SETUP_TEXT, disabled: true }), n('g', 'reply.http_get', { url: SETUP_URL, disabled: true })],
      [e('e1', 't', 'next', 'c'), e('e2', 'c', 'next', 'g')]
    );
    const shown = JSON.stringify(rehearse(flow));
    expect(shown).not.toContain('setup-required');
    for (const node of flow.nodes) expect(describeNode(english, node)).toBe('Off until set up');
  });

  test('every step type rehearses', () => {
    for (const spec of NODE_SPECS) {
      expect(rehearse(flowOf([n('n', spec.type)], []), { until: 'n' }).length).toBe(1);
    }
  });
});
