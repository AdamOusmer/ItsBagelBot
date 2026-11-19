// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { durationOutside, durationRange, fieldHint, fieldIssueMessage, fieldLabel, fieldOptions, fieldPlaceholder, fieldUnit, groupLabel, roleWarning, scopeWarning, stepLabel, stepNote, stepSummary, variableLabel } from './copy';
import { describe as describeNode, portLabel } from './describe';
import { english } from './english.testing';
import { fieldVisible, newFlow, NODE_SPECS, outputsFor, specFor, type FlowNode } from './model';
import type { Translate } from './messages';

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
    { type: 'cond.cooldown', key: 'name', hint: 'Naming this restarts it if it is running. Add time steps in any recipe can then extend it. Not available per viewer.' },
    { type: 'reply.cooldown_add', key: 'per', hint: 'Adds the time above for each full amount. Put 100 on a bits recipe to count every 100 bits. Leave empty to add it once per event.' },
    { type: 'reply.countdown_add', key: 'per', hint: 'Add this time for each full amount. Leave empty to add it once per event.' },
    { type: 'reply.countdown_add', key: 'name', hint: undefined },
    { type: 'reply.wait', key: 'seconds', hint: 'All waits in one run add up to 60 seconds at most.' },
    { type: 'reply.loop', key: 'times', hint: '1 to 100. A variable works too, like {args}.' },
    { type: 'reply.math', key: 'expression', hint: 'Use + - * / % ^ and round, floor, ceil, abs, min, max, random(a, b).' },
    { type: 'reply.http_get', key: 'jsonPath', hint: 'Dots between keys; brackets work too.' }
  ])('$type $key shows a hint only when translated', ({ type, key, hint }) => {
    const spec = specFor(type)!;
    const field = spec.fields.find((field) => field.key === key)!;
    expect(fieldHint(english, spec, field)).toBe(hint);
  });

  test.each([
    { op: 'upper', labels: [] },
    { op: 'replace', labels: ['Find', 'Replace with'] },
    { op: 'substring', labels: ['Skip', 'Keep'] },
    { op: 'split_pick', labels: ['Split on', 'Pick'], placeholder: '1, -1 for last, or random' },
    { op: 'contains', labels: ['Look for'] },
    { op: 'count', labels: ['Split on'] }
  ])('text $op shows only its operands', ({ op, labels, placeholder }) => {
    const spec = specFor('reply.text')!;
    const node = { config: { op } };
    const operands = spec.fields.filter((field) => field.labelBy === 'op');
    const shown = operands.filter((field) => fieldVisible(spec, node, field));
    expect({
      keys: operands.map((field) => field.key),
      labels: shown.map((field) => fieldLabel(english, spec, field, node)),
      placeholder: fieldPlaceholder(english, spec, operands[1], node)
    }).toEqual({ keys: ['arg1', 'arg2'], labels: [...labels], placeholder });
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
    const node = { id: 'step', type: spec.type, config: {}, x: 0, y: 0 };
    const ports = outputsFor(spec, node);
    const shown = [
      stepLabel(english, spec),
      stepSummary(english, spec),
      stepNote(english, spec),
      groupLabel(english, spec.group),
      ...spec.fields.flatMap((field) => [
        fieldLabel(english, spec, field),
        fieldPlaceholder(english, spec, field),
        fieldUnit(english, spec, field),
        ...fieldOptions(english, spec, field).map((option) => option.label)
      ]),
      ...ports.map((output) => portLabel(english, node, output.id)),
      ...[...(spec.provides ?? []), ...ports.flatMap((output) => output.provides ?? [])].map((variable) => variableLabel(english, variable))
    ].filter((text) => text !== undefined);
    expect(shown.filter(missing)).toEqual([]);
  });

  test.each([
    { paths: '2', labels: ['Path 1 (75%)', 'Path 2 (25%)'] },
    { paths: '3', labels: ['Path 1 (30%)', 'Path 2 (10%)', 'Path 3 (60%)'] }
  ])('$paths random paths use only their active weights', ({ paths, labels }) => {
    const spec = specFor('reply.random')!;
    const node = { id: 'r', type: spec.type, config: { paths, weight1: 3, weight2: 1, weight3: 6 }, x: 0, y: 0 };
    expect(outputsFor(spec, node).map((port) => portLabel(english, node, port.id))).toEqual(labels);
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

  test.each<{ config: FlowNode['config']; line: string }>([
    { config: { do: 'set', name: 'round2', value: '1' }, line: 'Set round2 to 1' },
    { config: { do: 'add', name: 'round2', amount: 2 }, line: 'Add 2 to round2' },
    { config: { do: 'subtract', name: 'round2', amount: 2 }, line: 'Subtract 2 from round2' },
    { config: { do: 'forget', name: 'round2' }, line: 'Forget round2' }
  ])('describes the variable $config.do action', ({ config, line }) => {
    expect(describeNode(english, node('reply.var', config))).toBe(line);
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

test.each([
  ['twitch.stream.online', 'viewer', true], ['twitch.stream.offline', 'viewer', true],
  ['countdown.finished', 'viewer', true], ['twitch.follow', 'viewer', false],
  ['twitch.unban_request', 'viewer', false], ['twitch.stream.online', 'channel', false]
])('%s shows the variable scope warning for %s: %s', (type, scope, warns) => {
  const flow = newFlow('Variables');
  flow.nodes = [{ id: 't', type, config: {}, x: 0, y: 0 }];
  const action: FlowNode = { id: 'v', type: 'reply.var', config: { scope }, x: 368, y: 0 };
  expect(Boolean(scopeWarning(english, flow, action))).toBe(warns);
});
