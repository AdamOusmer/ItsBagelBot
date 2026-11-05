// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { ACTION_TYPES, CONDITION_TYPES, TRIGGER_TYPES, type TypeDef } from './catalog';

const GO_DIR = join(import.meta.dir, '../../../../internal/domain/automation');
const registry = readFileSync(join(GO_DIR, 'registry.go'), 'utf8');

function goTypes(table: string): string[] {
  const body = registry.match(new RegExp(`${table} = map\\[string\\]entry\\{([\\s\\S]*?)\\n\\t\\}`));
  return [...(body?.[1] ?? '').matchAll(/"([a-z_]+)":/g)].map((m) => m[1]);
}

const groups: [string, string, TypeDef[]][] = [
  ['triggerRegistry', 'trigger', TRIGGER_TYPES],
  ['conditionRegistry', 'condition', CONDITION_TYPES],
  ['actionRegistry', 'action', ACTION_TYPES],
];

describe('catalog', () => {
  for (const [table, group, defs] of groups) {
    test(`${group} entries match the Go registry one to one`, () => {
      const go = goTypes(table);
      expect(go.length).toBeGreaterThan(0);
      expect(defs.map((d) => d.type).sort()).toEqual([...go].sort());
    });

    test(`${group} label keys follow the convention`, () => {
      for (const d of defs) {
        expect(d.labelKey).toBe(`automations.${group}.${d.type}`);
        for (const field of d.fields) expect(field.labelKey).toBe(`automations.field.${field.name}`);
      }
    });
  }

  test('every catalog entry is supported', () => {
    expect(ACTION_TYPES.every((d) => d.supported)).toBe(true);
    expect([...TRIGGER_TYPES, ...CONDITION_TYPES].every((d) => d.supported)).toBe(true);
  });
});
