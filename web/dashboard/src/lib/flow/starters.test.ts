// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { flowToDefinition, validateDefinition } from '@bagel/kit/automations';
import { english } from './english.testing';
import { legacyEditorHref, type FlowRef } from './ref';
import { STARTERS, starterFlow } from './starters';

test.each(STARTERS)('starter %s opens a fresh valid live draft', (id) => {
  const flow = starterFlow(id, english)!;
  const lowered = flowToDefinition(flow);
  if (!lowered.ok) throw new Error(JSON.stringify(lowered.issues));
  expect({ id: flow.id, enabled: flow.enabled, issues: validateDefinition(lowered.value) }).toEqual({ id: 'new', enabled: true, issues: [] });
  flow.nodes[0].config.changed = true;
  expect(starterFlow(id, english)?.nodes[0].config.changed).toBeUndefined();
});

test.each([null, '', 'missing'])('unknown starter %s keeps the blank draft', (id) => {
  expect(starterFlow(id, english)).toBeNull();
});

test.each<{ ref: FlowRef; href: string }>([
  { ref: { layer: 'automation', id: 'recipe/one' }, href: '/automations/recipe%2Fone' },
  { ref: { layer: 'command', name: 'hug' }, href: '/commands' },
  { ref: { layer: 'alert', alert: 'follow' }, href: '/modules/alerts' }
])('opens $href in the original editor', ({ ref, href }) => {
  expect(legacyEditorHref(ref)).toBe(href);
});
