// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, mock, test } from 'bun:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import * as svelte from 'svelte';
import { render } from 'svelte/server';
import { flowToDefinition } from '@bagel/kit/automations';
import { english } from '../../flow/english.testing';
import { fieldVisible, newFlow, NODE_SPECS, type Flow, type FlowNode } from '../../flow/model';
import { addNode } from '../../flow/ops.testing';
import { rehearseFlow } from '../../flow/rehearse';

mock.module('$app/env', () => ({ browser: false, dev: false }));
mock.module('$app/forms', () => ({ deserialize: JSON.parse, enhance: () => undefined }));
mock.module('$app/navigation', () => ({ afterNavigate: () => {}, beforeNavigate: () => {}, goto: () => {}, invalidate: () => {}, invalidateAll: () => {} }));
mock.module('$app/state', () => ({
  navigating: null, page: { data: { govee: Promise.resolve({ keyPresent: false, devices: [] }) } }, updated: { current: false }
}));
mock.module('virtual:bagel-i18n', () => ({ loaders: {}, localeNames: {}, locales: ['en'], sliced: false, urls: {} }));

const uiSvelte = createRequire(import.meta.resolve('@bagel/ui/svelte/Field.svelte')).resolve('svelte');
if (uiSvelte !== fileURLToPath(import.meta.resolve('svelte'))) mock.module(uiSvelte, () => svelte);

const { default: NodeInspector } = await import('./NodeInspector.svelte');

function inspect(flow: Flow, node: FlowNode): string {
  return render(NodeInspector, {
    props: { flow, selectedId: node.id, rehearsal: rehearseFlow(flow, english), onremove: () => {} }
  }).body;
}

const gateCases = NODE_SPECS.flatMap((spec) => {
  const gatedKeys = new Set(spec.fields.flatMap(({ when }) => {
    if (!when) return [];
    return 'key' in when ? [when.key] : when.map(({ key }) => key);
  }));
  return spec.fields.filter(({ key }) => gatedKeys.has(key)).flatMap((gate) => {
    const options = gate.type === 'select' ? gate.options : [false, true];
    return options.map((value) => ({ type: spec.type, spec, key: gate.key, value }));
  });
});

test.each(gateCases)('$type keeps all fields mounted with $key set to $value', ({ spec, key, value }) => {
  const flow = newFlow('Inspector');
  const node = addNode(flow, spec.type, { x: 0, y: 0 });
  node.config[key] = value;
  const fields = Array.from(inspect(flow, node).matchAll(/<fieldset\b([^>]*?)\bdata-field="([^"]+)"[^>]*>/g), ([, attributes, fieldKey]) => ({
    key: fieldKey, disabled: /\sdisabled\b/.test(attributes!), inert: /\sinert\b/.test(attributes!)
  }));
  const expected = spec.fields.map((field) => {
    const inactive = !fieldVisible(spec, node, field);
    return { key: field.key, disabled: inactive, inert: inactive };
  });
  expect(fields).toEqual(expected);
});

test('the cooldown name is retained when per viewer is toggled and only lowers for a channel cooldown', () => {
  const flow = newFlow('Inspector');
  const trigger = addNode(flow, 'twitch.follow', { x: 0, y: 0 });
  const gate = addNode(flow, 'cond.cooldown', { x: 368, y: 0 }, { from: trigger.id, port: 'next' });
  gate.config.name = 'break';
  const states = [false, true, false].map((perUser) => {
    gate.config.perUser = perUser;
    const saved = flowToDefinition(flow);
    if (!saved.ok) throw new Error(JSON.stringify(saved.issues));
    return { draft: gate.config.name, shown: /value="break"/.test(inspect(flow, gate)), params: saved.value.conditions?.[0]?.params };
  });
  expect(states).toEqual([
    { draft: 'break', shown: true, params: { global_s: 30, name: 'break' } },
    { draft: 'break', shown: true, params: { user_s: 30 } },
    { draft: 'break', shown: true, params: { global_s: 30, name: 'break' } }
  ]);
});
