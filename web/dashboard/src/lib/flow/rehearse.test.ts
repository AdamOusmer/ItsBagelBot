// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { NODE_SPECS, type Flow } from './model';
import { rehearseFlow, type RehearsalStep } from './rehearse';

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

const transcript = (flow: Flow, opts = {}) => rehearseFlow(flow, opts).map(said);

describe('rehearseFlow', () => {
  test('a command shows the viewer line, then the passing branch', () => {
    expect(transcript(hug)).toEqual([
      '!hug Lotti good luck',
      'Passes 85% of the time',
      'PrincessBarney gives Lotti a big warm hug.'
    ]);
  });

  test('a decision flips the branch', () => {
    const [, check, reply] = rehearseFlow(hug, { decisions: { a2: 'no' } });
    expect(check.kind === 'check' && check.taken).toBe('no');
    expect(said(reply)).toBe('The hug slipped away, PrincessBarney. Try again!');
  });

  test('each step names the wire it came through, on the taken branch', () => {
    expect(rehearseFlow(hug, { decisions: { a2: 'no' } }).map((s) => s.via)).toEqual([undefined, 'ae1', 'ae3']);
  });

  test('a compare evaluates against the trigger samples', () => {
    expect(transcript(sub)).toEqual([
      'PrincessBarney subscribed at tier 1',
      '14 is at least 12',
      'PrincessBarney has been here 14 months. Legend!'
    ]);
  });

  test('fan-out runs every step in order, effects included', () => {
    expect(transcript(follow)).toEqual(['PrincessBarney followed the channel', 'Welcome to the stream, PrincessBarney!', 'Give 100 points']);
  });

  test('a trigger floor raises the sample so the trigger would really fire', () => {
    const raid = flowOf([n('r1', 'twitch.raid', { minViewers: 500 })], []);
    expect(said(rehearseFlow(raid)[0])).toBe('PrincessBarney raided with 500 viewers');
  });

  test('until walks the branch that reaches the node, then stops', () => {
    const steps = rehearseFlow(hug, { until: 'a4' });
    expect(steps.map((s) => s.id)).toEqual(['a1', 'a2', 'a4']);
    expect(said(steps[2])).toContain('slipped away');
  });

  test('an unconnected message stands alone, trigger tokens marked unknown', () => {
    const loose = flowOf([n('c1', 'twitch.cheer'), n('c2', 'reply.chat', { message: 'Thanks for {bits} bits' })], []);
    const [step] = rehearseFlow(loose, { until: 'c2' });
    expect(step.kind === 'bot' && step.lines[0].segments.some((s) => s.kind === 'unknown' && s.text === '{bits}')).toBe(true);
  });

  test('a loop ends', () => {
    const looped = { ...follow, edges: [...follow.edges, e('back', 'f2', 'next', 'f2')] };
    expect(rehearseFlow(looped).length).toBe(3);
  });

  test('no trigger means nothing to rehearse', () => {
    expect(rehearseFlow(flowOf([], []))).toEqual([]);
  });

  test('a chat word trigger rehearses as the viewer saying it', () => {
    const [step] = rehearseFlow(flowOf([n('w', 'twitch.chat.word', { word: 'bagel' })], []), { until: 'w' });
    expect(step.kind === 'viewer' && step.text).toBe('bagel');
  });

  test('every step type rehearses', () => {
    for (const spec of NODE_SPECS) {
      expect(rehearseFlow(flowOf([n('n', spec.type)], []), { until: 'n' }).length).toBe(1);
    }
  });
});
