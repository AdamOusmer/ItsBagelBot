// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import {
  alertFields, alertLook, creditHeaders, listRule, tierWeights, typography, variationFields, widgetFields, type SettingField
} from './fields';
import { parseSettings } from './settings';
import type { WidgetKind } from './widget-types';

type Rule = { key: string; type: string; min?: number; max?: number; options?: string[]; fields?: Rule[] };

const table: { kinds: Record<string, { fields: Rule[] }> } = JSON.parse(
  readFileSync(new URL('../../../internal/domain/overlays/testdata/widget-settings.json', import.meta.url), 'utf8')
);

const find = (rules: Rule[] | undefined, key: string): Rule | undefined => rules?.find((rule) => rule.key === key);
const nested = (rules: Rule[] | undefined, ...path: string[]): Rule[] | undefined =>
  path.reduce<Rule[] | undefined>((current, key) => find(current, key)?.fields, rules);

const kinds = Object.keys(widgetFields) as WidgetKind[];
const alertEvent = (kind: WidgetKind) => nested(table.kinds[kind]?.fields, 'events');

const groups: { name: string; fields: SettingField[]; rules: Rule[] | undefined }[] = [
  ...kinds.map((kind) => ({ name: kind, fields: widgetFields[kind], rules: table.kinds[kind]?.fields })),
  { name: 'alertbox events', fields: alertFields, rules: alertEvent('alertbox') },
  { name: 'alertbox variations', fields: variationFields, rules: nested(table.kinds.alertbox?.fields, 'events', 'variations') },
  { name: 'alertbox overrides', fields: alertLook, rules: nested(table.kinds.alertbox?.fields, 'events', 'variations', 'overrides') },
  { name: 'eventlist rules', fields: listRule, rules: nested(table.kinds.eventlist?.fields, 'rules') },
  { name: 'goal weights', fields: tierWeights, rules: nested(table.kinds.goal?.fields, 'weights') },
  { name: 'credits headers', fields: creditHeaders, rules: nested(table.kinds.credits?.fields, 'headers') },
  { name: 'text style', fields: typography, rules: nested(table.kinds.text?.fields, 'style') }
];

const entries = groups.flatMap(({ name, fields, rules }) =>
  fields.map((field) => ({ name, label: `${name}.${field.key}`, field, rule: find(rules, field.key) }))
);

const accepts = (kind: WidgetKind, key: string, value: unknown): boolean => {
  try {
    parseSettings(kind, { [key]: value });
    return true;
  } catch {
    return false;
  }
};

describe('widget settings table shared with Go', () => {
  test('lists every widget kind', () => {
    expect(Object.keys(table.kinds).sort()).toEqual([...kinds].sort());
  });

  test.each(entries)('$label has the validator type and range', ({ field, rule }) => {
    expect({ type: rule?.type, min: rule?.min ?? 0, max: rule?.max, options: rule?.options }).toEqual({
      type: field.type,
      min: field.type === 'number' ? (field.min ?? 0) : 0,
      max: field.type === 'number' ? field.max : undefined,
      options: field.type === 'select' ? [...(field.options ?? [])] : undefined
    });
  });

  test.each(groups)('$name has no key the validator lacks', ({ fields, rules }) => {
    const structural = new Set(['object', 'map', 'array', 'scalars']);
    const known = new Set(fields.map((field) => field.key));
    expect((rules ?? []).filter((rule) => !known.has(rule.key) && !structural.has(rule.type)).map((rule) => rule.key)).toEqual([]);
  });

  test.each(entries.filter(({ name, field }) => widgetFields[name as WidgetKind] && field.type === 'number'))(
    '$label accepts its bounds and refuses beyond',
    ({ name, field }) => {
      const kind = name as WidgetKind;
      expect([accepts(kind, field.key, field.min), accepts(kind, field.key, field.max)]).toEqual([true, true]);
      expect(accepts(kind, field.key, (field.min ?? 0) - 1)).toBe(false);
    }
  );

  test.each(entries.filter(({ name, field }) => widgetFields[name as WidgetKind] && field.type === 'select'))(
    '$label accepts its options only',
    ({ name, field }) => {
      const kind = name as WidgetKind;
      expect([accepts(kind, field.key, 'zz-unknown'), (field.options ?? []).every((option) => accepts(kind, field.key, option))]).toEqual([false, true]);
    }
  );
});
