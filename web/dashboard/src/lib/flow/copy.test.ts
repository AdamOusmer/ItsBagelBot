// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { checkNote, durationOutside, durationRange, fieldHint, fieldIssueMessage, fieldLabel, fieldOptions, fieldPlaceholder, fieldUnit, groupLabel, outputLabel, roleWarning, stepLabel, stepSummary, variableLabel } from './copy';
import { describe as describeNode } from './describe';
import { english } from './english.testing';
import { newFlow, NODE_SPECS, specFor, type FlowNode } from './model';
import { issueMessage, type Translate } from './messages';

const missing = (text: string | undefined) => text === undefined || text.startsWith('flows.');

test.each([
  ['twitch.chat.command', false],
  ['countdown.finished', false],
  ['twitch.reward', true],
  ['twitch.raid', true]
])('%s warns only when the trigger has no role', (type, warns) => {
  const flow = newFlow('Roles');
  flow.nodes = [{ id: 't', type, config: {}, x: 0, y: 0 }];
  const role: FlowNode = { id: 'r', type: 'cond.role', config: { level: 'sub' }, x: 368, y: 0 };
  expect(Boolean(roleWarning(english, flow, role))).toBe(warns);
});

describe('step copy', () => {
  test.each([
    { type: 'cond.cooldown', key: 'name', hint: 'A named cooldown can be extended by Add time steps in any recipe. Channel wait only.' },
    { type: 'reply.cooldown_add', key: 'per', hint: 'Adds the time above for each full amount. Put 100 on a bits recipe to count every 100 bits. Leave empty to add it once per event.' },
    { type: 'reply.countdown_add', key: 'per', hint: 'Add this time for each full amount. Leave empty to add it once per event.' },
    { type: 'reply.countdown_add', key: 'name', hint: undefined }
  ])('$type $key shows a hint only when translated', ({ type, key, hint }) => {
    const spec = specFor(type)!;
    const field = spec.fields.find((field) => field.key === key)!;
    expect(fieldHint(english, spec, field)).toBe(hint);
  });

  test.each([
    { op: undefined, label: 'Uppercase input', placeholder: '{args}', variantOnly: undefined },
    { op: 'replace', label: 'Text to change', placeholder: 'Search text', variantOnly: 'Search text' },
    { op: 'split_pick', label: 'Text', placeholder: '{args}', variantOnly: undefined }
  ])('chooses field copy for $op with plain keys as fallback', ({ op, label, placeholder, variantOnly }) => {
    const spec = NODE_SPECS.find((entry) => entry.type === 'reply.text')!;
    const field = { ...spec.fields[1]!, labelBy: 'op' };
    const variants: Record<string, string> = {
      'flows.step.reply.text.field.input.upper.label': 'Uppercase input',
      'flows.step.reply.text.field.input.replace.label': 'Text to change',
      'flows.step.reply.text.field.input.replace.placeholder': 'Search text'
    };
    const t: Translate = (key, params) => variants[key] ?? english(key, params);
    const node: Pick<FlowNode, 'config'> = { config: op === undefined ? {} : { op } };
    expect({
      label: fieldLabel(t, spec, field, node),
      placeholder: fieldPlaceholder(t, spec, field, node),
      variantOnly: fieldPlaceholder(t, spec, { ...field, type: 'template', placeholder: undefined }, node)
    }).toEqual({ label, placeholder, variantOnly });
  });

  test.each([
    { type: 'reply.chat', fieldKey: 'message', code: 'required', expected: 'Message is missing.' },
    { type: 'twitch.chat.command', fieldKey: 'trigger', code: 'required', expected: 'Command is missing.' },
    { type: 'reply.chat', fieldKey: 'unknown', code: 'required', expected: 'A value is missing.' },
    { type: 'reply.chat', fieldKey: 'message', code: 'too_long', expected: 'A value is too long.' }
  ])('$type $fieldKey $code names the missing field when mapped', ({ type, fieldKey, code, expected }) => {
    const node: FlowNode = { id: 'n', type, x: 0, y: 0, config: {} };
    expect(fieldIssueMessage(english, { nodeId: node.id, fieldKey, code, path: 'nodes.n.config' }, node)).toBe(expected);
  });

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
    expect(durationRange(english, cooldown, field)).toBe('Use 0 seconds to 30 days.');
    expect([undefined, 0, 2_592_000].map((seconds) => durationOutside(field, seconds))).toEqual([false, false, false]);
    expect([-1, 2_592_001].map((seconds) => durationOutside(field, seconds))).toEqual([true, true]);
  });

  test.each<{ config: FlowNode['config']; line: string }>([
    { config: { channelSeconds: 30, viewerSeconds: 0 }, line: 'Once every 30 seconds' },
    { config: { channelSeconds: 0, viewerSeconds: 30 }, line: 'Once every 30 seconds per viewer' },
    { config: { channelSeconds: 30, viewerSeconds: 120 }, line: 'Once every 30 seconds, and every 2 minutes per viewer' },
    { config: { channelSeconds: 60 }, line: 'Once every 1 minute' },
    { config: { channelSeconds: 7200 }, line: 'Once every 2 hours' },
    { config: { channelSeconds: 86_400 }, line: 'Once every 1 day' },
    { config: { channelSeconds: 604_800 }, line: 'Once every 7 days' }
  ])('cooldown $config reads $line', ({ config, line }) => {
    expect(describeNode(english, { id: 'c', type: 'cond.cooldown', x: 0, y: 0, config })).toBe(line);
  });

  test.each<{ type: string; config: FlowNode['config']; note: string | null }>([
    { type: 'cond.role', config: { level: 'everyone' }, note: 'Everyone passes, so this check never blocks.' },
    { type: 'cond.role', config: { level: '' }, note: 'Everyone passes, so this check never blocks.' },
    { type: 'cond.role', config: { level: 'mod' }, note: null },
    { type: 'cond.chance', config: { percent: 99 }, note: null },
    { type: 'cond.chance', config: { percent: 100 }, note: 'Always passes.' },
    { type: 'cond.chance', config: { percent: '100' }, note: 'Always passes.' },
    { type: 'cond.chance', config: { percent: 101 }, note: 'Always passes.' }
  ])('$type with $config shows $note', ({ type, config, note }) => {
    expect(checkNote(english, { id: 'n', type, x: 0, y: 0, config })).toBe(note);
  });

  test('unknown ports ask to reconnect the connection', () => {
    expect(issueMessage(english, { path: 'nodes.c', code: 'unknown_port' }))
      .toBe('A connection leaves this step from a port it does not have. Reconnect it.');
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
