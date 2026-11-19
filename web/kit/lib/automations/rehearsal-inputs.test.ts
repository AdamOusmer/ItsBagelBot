// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { extractJson } from './rehearsal-json';
import { defaultScenario, rehearseDefinition } from './rehearsal';
import type { Definition } from './types';

test.each([
  { name: 'tabs after text', current: `x${'\t'.repeat(200_000)}!`, value: '0', expected: 'greater' },
  { name: 'leading and trailing whitespace', current: `${'\t'.repeat(200_000)}1 \r\n`, value: '01', expected: 'equal' },
  { name: 'whitespace only', current: '\t'.repeat(200_000), value: '0', expected: 'less' },
  { name: 'invalid long decimal', current: `${'1'.repeat(200_000)}!`, value: '2', expected: 'less' },
  { name: 'non-ASCII space stays text', current: '\u00a01', value: '1', expected: 'greater' },
  { name: 'decimal exponent', current: '+.1e2', value: '10.', expected: 'equal' }
])('variable comparison handles $name in linear time', ({ current, value, expected }) => {
  const definition: Definition = {
    version: 1, trigger: { type: 'manual' },
    actions: ['lt', 'eq', 'gt'].map((op, index) => ({
      type: 'if', params: {
        condition: { type: 'var', params: { name: 'score', op, value } },
        then: [{ type: 'chat', params: { text: ['less', 'equal', 'greater'][index] } }]
      }
    }))
  };
  const scenario = { ...defaultScenario(definition), state: { channelVars: { score: current } } };
  const start = performance.now();
  const result = rehearseDefinition(definition, scenario);
  expect(result.events.flatMap((event) => event.lines ?? [])).toEqual([expected]);
  expect(performance.now() - start).toBeLessThan(1000);
});

test.each([
  { name: 'escaped quotes', value: '"'.repeat(200_000) },
  { name: 'escaped backslashes', value: '\\'.repeat(200_000) },
  { name: 'alternating escapes', value: '\\"\n\t'.repeat(50_000) }
])('JSON extraction handles long $name in linear time', ({ value }) => {
  const response = JSON.stringify({ ignored: value, items: [{ value }] });
  const start = performance.now();
  expect(extractJson(response, ['items', '0', 'value'])).toBe(value);
  expect(performance.now() - start).toBeLessThan(1000);
});

test.each([
  { name: 'unterminated escaped quotes', response: `"${'\\"'.repeat(200_000)}` },
  { name: 'unterminated backslashes', response: `{"value":"${'\\\\'.repeat(200_000)}` }
])('JSON extraction rejects $name promptly', ({ response }) => {
  const start = performance.now();
  expect(() => extractJson(response, ['value'])).toThrow();
  expect(performance.now() - start).toBeLessThan(1000);
});
