// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, mock, test } from 'bun:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import * as svelte from 'svelte';
import { render } from 'svelte/server';
import { flowToDefinition } from '@bagel/kit/automations';
import { english } from '../../flow/english.testing';
import { fieldMounted, fieldVisible, newFlow, NODE_SPECS, type FieldSpec, type Flow, type FlowNode } from '../../flow/model';
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

const gateValues: Partial<Record<FieldSpec['type'], readonly FlowNode['config'][string][]>> = {
  target: ['viewer', 'mention', 'user'],
  duration: [0, 30],
  text: ['', '!timeleft']
};

const gateCases = NODE_SPECS.flatMap((spec) => {
  const gatedKeys = new Set(spec.fields.flatMap(({ when }) => {
    if (!when) return [];
    return 'key' in when ? [when.key] : when.map(({ key }) => key);
  }));
  return spec.fields.filter(({ key }) => gatedKeys.has(key)).flatMap((gate) => {
    const options = gate.type === 'select' ? gate.options : gateValues[gate.type] ?? [false, true];
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
  const expected = spec.fields.filter((field) => fieldMounted(spec, node, field)).map((field) => {
    const inactive = !fieldVisible(spec, node, field);
    return { key: field.key, disabled: inactive, inert: inactive };
  });
  expect(fields).toEqual(expected);
});

test.each([
  { trigger: 'twitch.chat.command', config: {}, modes: ['viewer', 'mention', 'user'], checked: 'mention' },
  { trigger: 'twitch.follow', config: {}, modes: ['viewer', 'user'], checked: 'viewer' },
  { trigger: 'twitch.stream.offline', config: {}, modes: ['user'], checked: 'user' },
  { trigger: 'twitch.follow', config: { who: undefined, target: '{user}' }, modes: ['viewer', 'user', 'login'], checked: 'login' }
])('$trigger shows the eligible target choices with $checked selected', ({ trigger, config, modes, checked }) => {
  const flow = newFlow('Inspector targets');
  const root = addNode(flow, trigger, { x: 0, y: 0 });
  const step = addNode(flow, 'reply.timeout', { x: 368, y: 0 }, { from: root.id, port: 'next' });
  if (config.target) {
    delete step.config.who;
    step.config.target = config.target;
  }
  const html = inspect(flow, step);
  const buttons = Array.from(html.matchAll(/role="radio" aria-checked="(true|false)"[^>]*value="([^"]+)"/g), ([, active, mode]) => ({ mode, checked: active === 'true' }));
  expect(buttons).toEqual(modes.map((mode) => ({ mode, checked: mode === checked })));
});

test.each<{ config: FlowNode['config']; waits: Record<string, number>; params: FlowNode['config'] }>([
  { config: { channelSeconds: 45, viewerSeconds: 0 }, waits: { channelSeconds: 45, viewerSeconds: 0 }, params: { global_s: 45, name: 'break' } },
  { config: { channelSeconds: 45, viewerSeconds: 15 }, waits: { channelSeconds: 45, viewerSeconds: 15 }, params: { global_s: 45, user_s: 15 } },
  { config: { seconds: 45, perUser: false }, waits: { channelSeconds: 45, viewerSeconds: 0 }, params: { global_s: 45, name: 'break' } },
  { config: { seconds: 45, perUser: true }, waits: { channelSeconds: 0, viewerSeconds: 45 }, params: { user_s: 45 } }
])('cooldown $config shows both waits and keeps the name mounted', ({ config, waits, params }) => {
  const flow = newFlow('Inspector');
  const trigger = addNode(flow, 'twitch.follow', { x: 0, y: 0 });
  const gate = addNode(flow, 'cond.cooldown', { x: 368, y: 0 }, { from: trigger.id, port: 'next' });
  gate.config = { ...config, name: 'break' };
  const html = inspect(flow, gate);
  const inputs = html.matchAll(/<input\b(?=[^>]*\bid="[^"]*-(channelSeconds|viewerSeconds)")(?=[^>]*\bvalue="(\d+)")[^>]*>/g);
  const saved = flowToDefinition(flow);
  if (!saved.ok) throw new Error(JSON.stringify(saved.issues));
  expect({
    waits: Object.fromEntries(Array.from(inputs, ([, key, value]) => [key, Number(value)])),
    nameMounted: /value="break"/.test(html),
    params: saved.value.conditions?.[0]?.params
  }).toEqual({ waits, nameMounted: true, params });
});
