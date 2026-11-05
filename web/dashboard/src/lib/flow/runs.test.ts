// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { english } from './english.testing';
import type { Flow } from './model';
import { explainRun, explainStep, runStepRows } from './runs';

const flow: Flow = {
  id: 'f',
  name: 'f',
  enabled: true,
  nodes: [
    { id: 't', type: 'twitch.follow', x: 0, y: 0, config: {} },
    { id: 'l', type: 'reply.loop', x: 0, y: 0, config: { times: '3' } },
    { id: 'g', type: 'reply.http_get', x: 0, y: 0, config: { url: 'https://api.example.com/a' } },
    { id: 'a', type: 'reply.chat', x: 0, y: 0, config: { message: 'worked' } },
    { id: 'b', type: 'reply.chat', x: 0, y: 0, config: { message: 'failed' } },
    { id: 'c', type: 'reply.chat', x: 0, y: 0, config: { message: 'after' } }
  ],
  edges: [
    { id: 'e1', from: 't', port: 'next', to: 'l' },
    { id: 'e2', from: 'l', port: 'body', to: 'g' },
    { id: 'e3', from: 'g', port: 'ok', to: 'a' },
    { id: 'e4', from: 'g', port: 'failure', to: 'b' },
    { id: 'e5', from: 'l', port: 'done', to: 'c' }
  ]
};

describe('run step rows', () => {
  test('per-iteration records collapse by step with a count and the worst status', () => {
    const rows = runStepRows(
      english,
      [
        { index: 0, type: 'http_get', status: 'ok' },
        { index: 0, type: 'http_get', status: 'failed', detail: 'timeout' },
        { index: 0, type: 'http_get', status: 'ok' },
        { index: 1, type: 'chat', status: 'ok' }
      ],
      flow
    );
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ type: 'http_get', count: 3, status: 'failed', detail: 'timeout', name: 'Read from a web address', line: 'https://api.example.com/a' });
    expect(rows[1]).toMatchObject({ type: 'chat', count: 1, status: 'ok', name: 'Send chat message', line: 'after' });
  });

  test('same-type steps on different paths list every step they could be, not a guess', () => {
    const rows = runStepRows(english, [{ index: 0, type: 'chat', status: 'ok' }], flow);
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ name: 'Send chat message', line: 'One of: worked, failed', count: 1 });
  });

  test('same-type steps on one path each get their own row, in order, repeating under a loop', () => {
    const chain: Flow = {
      ...flow,
      nodes: [
        { id: 't', type: 'twitch.follow', x: 0, y: 0, config: {} },
        { id: 'l', type: 'reply.loop', x: 0, y: 0, config: { times: '2' } },
        { id: 'a', type: 'reply.chat', x: 0, y: 0, config: { message: 'first' } },
        { id: 'b', type: 'reply.chat', x: 0, y: 0, config: { message: 'second' } }
      ],
      edges: [
        { id: 'e1', from: 't', port: 'next', to: 'l' },
        { id: 'e2', from: 'l', port: 'body', to: 'a' },
        { id: 'e3', from: 'a', port: 'next', to: 'b' }
      ]
    };
    const rows = runStepRows(
      english,
      [
        { index: 0, type: 'chat', status: 'ok' },
        { index: 0, type: 'chat', status: 'failed', detail: 'text_error' },
        { index: 0, type: 'chat', status: 'ok' },
        { index: 0, type: 'chat', status: 'ok' }
      ],
      chain
    );
    expect(rows).toMatchObject([
      { line: 'first', count: 2, status: 'ok' },
      { line: 'second', count: 2, status: 'failed', detail: 'text_error' }
    ]);
  });

  test('without a graph the rows still group', () => {
    const rows = runStepRows(english, [
      { index: 2, type: 'math', status: 'ok' },
      { index: 2, type: 'math', status: 'ok' }
    ]);
    expect(rows).toMatchObject([{ count: 2, name: null }]);
  });
});

describe('plain words', () => {
  test('known details get a sentence and an unknown failure names what came back', () => {
    expect(explainStep(english, 'failed', 'math_error')).toContain('math');
    expect(explainStep(english, 'failed', 'reward_not_manageable')).toContain('rewards made here');
    expect(explainStep(english, 'failed', 'reward_not_found')).toContain('no longer exists');
    expect(explainStep(english, 'failed', 'reward_rejected')).toContain('turned down');
    expect(explainStep(english, 'failed', 'reward_unconfirmed')).toContain('several tries');
    expect(explainStep(english, 'ok', 'no_overlay')).toContain('overlay');
    expect(explainStep(english, 'ok', 'queued')).toContain('Sent to Twitch');
    expect(explainStep(english, 'failed', 'timeout')).toBe('This step failed: timeout.');
    expect(explainStep(english, 'failed', '')).toBe('This step failed.');
    expect(explainStep(english, 'ok', '')).toBe('');
  });

  test('a detail named like an object member is not looked up as one', () => {
    expect(explainStep(english, 'failed', 'constructor')).toBe('This step failed: constructor.');
  });

  test('a run stopped by a condition and queue outcomes read as sentences', () => {
    expect(explainRun(english, 'condition:0:perm')).toBe('This run stopped at the first check.');
    expect(explainRun(english, 'queue_timeout')).toContain('5 minutes');
    expect(explainRun(english, undefined)).toBe('');
  });
});
