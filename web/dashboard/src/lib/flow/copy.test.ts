// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { durationOutside, durationRange, fieldLabel, fieldOptions, fieldPlaceholder, fieldUnit, groupLabel, outputLabel, stepLabel, stepSummary, variableLabel } from './copy';
import { describe as describeNode } from './describe';
import { english } from './english.testing';
import { NODE_SPECS, type FlowNode } from './model';

const missing = (text: string | undefined) => text === undefined || text.startsWith('flows.');

describe('step copy', () => {
  test.each(NODE_SPECS.map((spec) => [spec.type, spec] as const))('%s has English for everything it shows', (_type, spec) => {
    const shown = [
      stepLabel(english, spec),
      stepSummary(english, spec),
      groupLabel(english, spec.group),
      ...spec.fields.flatMap((field) => [
        fieldLabel(english, spec, field),
        fieldPlaceholder(english, spec, field),
        fieldUnit(english, spec, field),
        ...fieldOptions(english, spec, field).map((option) => option.label)
      ]),
      ...(spec.outputs ?? []).map((output) => outputLabel(english, spec, output.id)),
      ...[...(spec.provides ?? []), ...(spec.outputs ?? []).flatMap((output) => output.provides ?? [])].map((variable) => variableLabel(english, variable))
    ].filter((text) => text !== undefined);
    expect(shown.filter(missing)).toEqual([]);
  });

  test('a field with no placeholder or unit asks for neither', () => {
    const trigger = NODE_SPECS.find((spec) => spec.type === 'twitch.reward')!;
    expect(fieldPlaceholder(english, trigger, trigger.fields[0])).toBeUndefined();
    expect(fieldUnit(english, trigger, trigger.fields[0])).toBeUndefined();
  });

  test('the cooldown takes up to 30 days and says so when a value is outside', () => {
    const cooldown = NODE_SPECS.find((spec) => spec.type === 'cond.cooldown')!;
    const field = cooldown.fields[0];
    if (field.type !== 'duration') throw new Error('cooldown is not a duration field');
    expect(durationRange(english, cooldown, field)).toBe('Use 1 second to 30 days.');
    expect([undefined, 1, 2_592_000].map((seconds) => durationOutside(field, seconds))).toEqual([false, false, false]);
    expect([0, 2_592_001].map((seconds) => durationOutside(field, seconds))).toEqual([true, true]);
  });

  test.each([
    [59, 'Wait 59 seconds between runs'],
    [60, 'Wait 1 minute between runs'],
    [7200, 'Wait 2 hours between runs'],
    [86_400, 'Wait 1 day between runs'],
    [604_800, 'Wait 7 days between runs']
  ])('a %d second cooldown reads %s', (seconds, line) => {
    expect(describeNode(english, { id: 'c', type: 'cond.cooldown', x: 0, y: 0, config: { seconds } })).toBe(line);
  });

  test('the wait unit names the longest wait', () => {
    const wait = NODE_SPECS.find((spec) => spec.type === 'reply.wait')!;
    expect(fieldUnit(english, wait, wait.fields[0])).toMatch(/^seconds, max \d+$/);
  });
});

describe('describe', () => {
  const node = (type: string, config: FlowNode['config'] = {}): FlowNode => ({ id: 'n', type, x: 0, y: 0, config });

  test('an unfilled step reads as its summary', () => {
    expect(describeNode(english, node('twitch.cheer'))).toBe('Bits are cheered');
  });

  test('counts take the singular only at one', () => {
    expect(describeNode(english, node('reply.wait', { seconds: 1 }))).toBe('Pause 1 second');
    expect(describeNode(english, node('reply.wait', { seconds: 5 }))).toBe('Pause 5 seconds');
    expect(describeNode(english, node('reply.points', { amount: -1 }))).toBe('Take 1 point');
  });

  test('lights without a name fall back to the generic phrase', () => {
    expect(describeNode(english, node('reply.lights', { mode: 'color', color: '#fff' }))).toBe('Set the lights to #fff');
  });

  test('a compare with an unknown operator still reads', () => {
    expect(describeNode(english, node('cond.compare', { left: 'a', right: 'b', op: 'nope' }))).toBe('a is b');
  });
});
