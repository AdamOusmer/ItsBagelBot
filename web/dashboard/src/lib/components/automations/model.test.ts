// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  ACTION_TYPES,
  CODE,
  CONDITION_TYPES,
  TRIGGER_TYPES,
  validateDefinition,
  type Definition
} from '@bagel/kit/automations';
import { readConsoleTree } from '../../../../../kit/lib/i18n/tree-fs';
import type { MessageTree } from '../../../../../kit/lib/i18n/types';
import { blankDefinition, makeAction, makeCondition, makeTrigger, normalizeDefinition } from './model';

const STRUCTURAL_CODES = ['unknown_field', 'invalid_format', 'invalid_json', 'unknown_type', 'unsupported_version'];

function structural(def: Definition): string[] {
  return validateDefinition(def)
    .filter((i) => STRUCTURAL_CODES.includes(i.code))
    .map((i) => `${i.path}:${i.code}`);
}

describe('editor defaults', () => {
  test('a blank definition is valid', () => {
    expect(validateDefinition(blankDefinition())).toEqual([]);
  });

  test.each(TRIGGER_TYPES.map((d) => d.type))('trigger %s defaults decode', (type) => {
    expect(structural({ ...blankDefinition(), trigger: makeTrigger(type) })).toEqual([]);
  });

  test.each(CONDITION_TYPES.map((d) => d.type))('condition %s defaults decode', (type) => {
    expect(structural({ ...blankDefinition(), conditions: [makeCondition(type)] })).toEqual([]);
  });

  test.each(ACTION_TYPES.map((d) => d.type))('action %s defaults decode', (type) => {
    expect(structural({ ...blankDefinition(), actions: [makeAction(type)] })).toEqual([]);
  });

  test('a minimal chat action turns valid once its text is filled', () => {
    const chat = makeAction('chat');
    chat.params!.text = 'hello {user}';
    expect(validateDefinition({ ...blankDefinition(), actions: [chat] })).toEqual([]);
  });

  test('normalising fills the optional containers a stored definition may omit', () => {
    const stored = { version: 1, trigger: { type: 'manual' }, actions: [{ type: 'if', params: { condition: { type: 'live' } } }] };
    const def = normalizeDefinition(stored as unknown as Definition);
    expect(def.conditions).toEqual([]);
    expect(def.trigger.params).toEqual({});
    expect(def.actions![0].params).toMatchObject({ then: [], else: [], condition: { type: 'live', params: {} } });
  });
});

function lookup(tree: MessageTree, key: string): unknown {
  return key.split('.').reduce<unknown>((node, part) => (node as Record<string, unknown> | undefined)?.[part], tree);
}

describe.each(['en', 'fr'])('%s catalog copy', (locale) => {
  const tree = readConsoleTree(locale);
  const defs = [...TRIGGER_TYPES, ...CONDITION_TYPES, ...ACTION_TYPES];

  test('every type and field label exists', () => {
    const keys = defs.flatMap((d) => [d.labelKey, ...d.fields.map((f) => f.labelKey)]);
    const missing = keys.filter((k) => typeof lookup(tree, k) !== 'string');
    expect(missing).toEqual([]);
  });

  test('every validation code has a message, including the owner-only ones', () => {
    const codes = [...Object.values(CODE), 'duplicate', 'unknown_automation'];
    expect(codes.filter((c) => typeof lookup(tree, `automations.issue.${c}`) !== 'string')).toEqual([]);
  });

  test('every enum value and event kind has a label', () => {
    const keys = defs.flatMap((d) =>
      d.fields
        .filter((f) => f.values && !f.values.every((v) => /^\d+$/.test(v)))
        .flatMap((f) => f.values!.map((v) => `automations.enum.${f.name}.${v || 'none'}`))
    );
    const missing = keys.filter((k) => typeof lookup(tree, k) !== 'string');
    expect(missing).toEqual([]);
  });
});
