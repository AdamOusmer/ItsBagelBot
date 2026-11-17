// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { exportFlows, planFlowImport, serializeFlowFile } from './flow-share';
import { MAX_DEFINITION_BYTES, MAX_FLOWS_PER_FILE, MAX_FLOW_FILE_BYTES } from './limits';
import { largeRecipe, sizedRecipe } from './testdata/large-recipe';
import { parseDefinition } from './validate';

test.each([
  { name: 'mixed 1,000-effect recipe', definition: largeRecipe(), codes: [] },
  { name: 'definition at the byte limit', definition: sizedRecipe(), codes: [] },
  { name: 'definition one byte over the limit', definition: sizedRecipe(MAX_DEFINITION_BYTES + 1), codes: ['too_long'] }
])('parses $name', ({ definition, codes }) => {
  expect(parseDefinition(JSON.stringify(definition)).issues.map((issue) => issue.code)).toEqual([...codes]);
});

test('a full channel of maximum-size recipes exports and imports', () => {
  const definition = sizedRecipe();
  const rows = Array.from({ length: MAX_FLOWS_PER_FILE }, (_, index) => ({ id: String(index), name: `Recipe ${index}`, definition }));
  const { file, report } = exportFlows(rows);
  const text = serializeFlowFile(file);
  expect([file.flows.length, report.flatMap((entry) => entry.issues)]).toEqual([MAX_FLOWS_PER_FILE, []]);
  expect(new TextEncoder().encode(text).length).toBeLessThanOrEqual(MAX_FLOW_FILE_BYTES);
  const plan = planFlowImport(text, { count: 0, names: [], commands: [], alerts: [] });
  expect(plan.ok ? plan.entries.map((entry) => entry.status) : plan.issues).toEqual(Array(MAX_FLOWS_PER_FILE).fill('ready'));
});
