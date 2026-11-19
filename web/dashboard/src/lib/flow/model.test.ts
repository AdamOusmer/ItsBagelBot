// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { flowToDefinition, validateDefinition } from '@bagel/kit/automations';
import { fieldVisible, newFlow, specFor, variablesFor, type FieldSpec, type FlowNode, type NodeSpec } from './model';
import { addNode } from './ops.testing';

const gatedField: FieldSpec = {
  key: 'value', type: 'number', when: [{ key: 'mode', value: ['slow', 'followers'] }, { key: 'on', value: true }]
};

const gateSpec: NodeSpec = {
  type: 'reply.chat_mode', kind: 'reply', group: 'chat', slug: 'chatMode', icon: 'clock',
  fields: [
    { key: 'mode', type: 'select', options: ['slow', 'followers', 'emote'] },
    { key: 'on', type: 'toggle', initial: true },
    gatedField
  ]
};

test.each<{ config: FlowNode['config']; visible: boolean }>([
  { config: {}, visible: true },
  { config: { mode: 'slow', on: true }, visible: true },
  { config: { mode: 'followers', on: true }, visible: true },
  { config: { mode: 'slow', on: false }, visible: false },
  { config: { mode: 'emote', on: true }, visible: false },
  { config: { mode: 'emote', on: false }, visible: false }
])('gates $config match all conditions: $visible', ({ config, visible }) => {
  expect(fieldVisible(gateSpec, { config }, gatedField)).toBe(visible);
});

test.each<{ config: FlowNode['config']; visible: string[] }>([
  { config: {}, visible: ['name'] },
  { config: { viewerSeconds: 0, leftCommand: '!timeleft' }, visible: ['name', 'leftReply', 'readyReply'] },
  { config: { viewerSeconds: '0', leftCommand: '' }, visible: ['name'] },
  { config: { viewerSeconds: '', leftCommand: '  ' }, visible: ['name'] },
  { config: { viewerSeconds: 30, leftCommand: '!timeleft' }, visible: ['leftReply', 'readyReply'] },
  { config: { seconds: 30, perUser: true }, visible: [] },
  { config: { seconds: 30, perUser: false }, visible: ['name'] }
])('cooldown $config shows optional fields $visible', ({ config, visible }) => {
  const spec = specFor('cond.cooldown')!;
  const active = spec.fields.filter((field) => field.when && fieldVisible(spec, { config }, field));
  expect(active.map((field) => field.key)).toEqual(visible);
});

const subscriptions = [
  { variant: undefined, tokens: ['{user}', '{user.id}', '{tier}'], event: 'channel.subscribe', text: '{user} {user.id} {event.tier}' },
  { variant: 'any', tokens: ['{user}', '{user.id}', '{tier}'], event: 'channel.subscribe', text: '{user} {user.id} {event.tier}' },
  { variant: 'new', tokens: ['{user}', '{user.id}', '{tier}'], event: 'channel.subscribe', text: '{user} {user.id} {event.tier}' },
  { variant: 'resub', tokens: ['{user}', '{user.id}', '{tier}', '{months}'], event: 'channel.subscription.message', text: '{user} {user.id} {event.tier} {event.months}' },
  { variant: 'gift', tokens: ['{user}', '{user.id}', '{tier}', '{count}'], event: 'channel.subscription.gift', text: '{user} {user.id} {event.tier} {event.amount}' }
];

test.each(subscriptions)('subscription $variant offers only tokens that save', ({ variant, tokens, event, text }) => {
  const flow = newFlow('Subscriptions');
  const root = addNode(flow, 'twitch.sub', { x: 0, y: 0 });
  root.config = variant === undefined ? {} : { variant };
  const wait = addNode(flow, 'reply.wait', { x: 0, y: 0 }, { from: root.id, port: 'next' });
  const chat = addNode(flow, 'reply.chat', { x: 0, y: 0 }, { from: wait.id, port: 'next' });
  expect(variablesFor(flow, chat.id).map((variable) => variable.token)).toEqual(tokens);
  chat.config.message = tokens.join(' ');
  const result = flowToDefinition(flow);
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  expect({ trigger: result.value.trigger.params?.kind, chat: result.value.actions?.[1].params?.text }).toEqual({ trigger: event, chat: text });
  expect(validateDefinition(result.value)).toEqual([]);
});

test('changing subscription variant refreshes the available tokens', () => {
  const flow = newFlow('Subscriptions');
  const root = addNode(flow, 'twitch.sub', { x: 0, y: 0 });
  const chat = addNode(flow, 'reply.chat', { x: 0, y: 0 }, { from: root.id, port: 'next' });
  const available = subscriptions.map(({ variant }) => {
    root.config = variant === undefined ? {} : { variant };
    return variablesFor(flow, chat.id).map((variable) => variable.token);
  });
  expect(available).toEqual(subscriptions.map(({ tokens }) => tokens));
});
