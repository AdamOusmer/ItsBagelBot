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

  test('two drawn steps of one type under a single action keep the type name, not a guess', () => {
    const [row] = runStepRows(english, [{ index: 0, type: 'chat', status: 'ok' }], flow);
    expect(row).toMatchObject({ name: null, line: null });
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
    expect(explainStep(english, 'ok', 'no_overlay')).toContain('overlay');
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
