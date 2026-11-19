// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { withDraftNames, type ChoiceSource } from './choices';
import { specFor, type FlowNode } from './model';

const draftCases: { source: ChoiceSource; type: string; picker?: FlowNode['type']; config: FlowNode['config']; names: string[] }[] = [
  { source: 'namedCountdowns', type: 'reply.countdown_start', config: { name: ' Tea ' }, names: ['tea'] },
  { source: 'namedCooldowns', type: 'cond.cooldown', config: { name: 'tea' }, names: ['tea'] },
  { source: 'namedCooldowns', type: 'cond.cooldown', config: { name: 'tea', perUser: true }, names: [] },
  { source: 'namedCountdowns', type: 'reply.countdown_start', config: { name: 'tea', disabled: true }, names: [] },
  { source: 'namedCountdowns', type: 'reply.countdown_add', config: { name: 'tea' }, names: [] },
  { source: 'namedCountdowns', type: 'reply.countdown_start', config: { name: 'tea_time' }, names: [] },
  { source: 'counters', type: 'reply.countdown_start', config: { name: 'tea' }, names: [] },
  { source: 'namedCountdowns', type: 'reply.countdown_start', config: { name: 'Clock2' }, names: ['clock2'] },
  { source: 'namedCooldowns', type: 'cond.cooldown', config: { name: 'Break2' }, names: ['break2'] },
  { source: 'namedCountdowns', picker: 'reply.countdown_stop', type: 'reply.countdown_start', config: { name: 'Subathon2' }, names: ['subathon2'] },
  { source: 'namedCooldowns', picker: 'reply.cooldown_reset', type: 'cond.cooldown', config: { name: 'Raffle2' }, names: ['raffle2'] }
];

test.each(draftCases)('$source includes draft $type with $config', ({ source, type, config, names, picker }) => {
  const node: FlowNode = { id: 'timer', type, config, x: 0, y: 0 };
  const list = { options: [{ value: 'saved', label: 'Saved timer' }], problem: 'unavailable' as const };
  const field = picker ? specFor(picker)?.fields.find((field) => field.key === 'name') : undefined;
  const resolved = picker ? field?.type === 'choice' && field.source : source;
  if (!resolved) throw new Error(`Missing name picker for ${picker}`);
  expect(withDraftNames(list, resolved, [node, node])).toEqual({
    options: [...list.options, ...names.map((name) => ({ value: name, label: name }))], problem: 'unavailable'
  });
});
