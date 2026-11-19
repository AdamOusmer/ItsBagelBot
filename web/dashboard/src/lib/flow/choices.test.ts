// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { withDraftNames, type ChoiceSource } from './choices';
import type { FlowNode } from './model';

const draftCases: { source: ChoiceSource; type: string; config: FlowNode['config']; names: string[] }[] = [
  { source: 'namedCountdowns', type: 'reply.countdown_start', config: { name: ' Tea ' }, names: ['tea'] },
  { source: 'namedCooldowns', type: 'cond.cooldown', config: { name: 'tea' }, names: ['tea'] },
  { source: 'namedCooldowns', type: 'cond.cooldown', config: { name: 'tea', perUser: true }, names: [] },
  { source: 'namedCountdowns', type: 'reply.countdown_start', config: { name: 'tea', disabled: true }, names: [] },
  { source: 'namedCountdowns', type: 'reply.countdown_add', config: { name: 'tea' }, names: [] },
  { source: 'namedCountdowns', type: 'reply.countdown_start', config: { name: 'tea_time' }, names: [] },
  { source: 'counters', type: 'reply.countdown_start', config: { name: 'tea' }, names: [] },
  { source: 'namedCountdowns', type: 'reply.countdown_start', config: { name: 'Clock2' }, names: ['clock2'] },
  { source: 'namedCooldowns', type: 'cond.cooldown', config: { name: 'Break2' }, names: ['break2'] }
];

test.each(draftCases)('$source includes draft $type with $config', ({ source, type, config, names }) => {
  const node: FlowNode = { id: 'timer', type, config, x: 0, y: 0 };
  const list = { options: [{ value: 'saved', label: 'Saved timer' }], problem: 'unavailable' as const };
  expect(withDraftNames(list, source, [node, node])).toEqual({
    options: [...list.options, ...names.map((name) => ({ value: name, label: name }))], problem: 'unavailable'
  });
});
