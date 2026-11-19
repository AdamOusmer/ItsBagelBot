// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { flowToDefinition, validateDefinition } from '@bagel/kit/automations';
import { fieldVisible, newFlow, variablesFor, type FieldSpec, type FlowNode, type NodeSpec } from './model';
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

const subscriptions = [
  { variant: undefined, tokens: ['{user}', '{user.id}', '{tier}'], event: 'channel.subscribe', text: '{user} {user.id} {event.tier}' },
  { variant: 'any', tokens: ['{user}', '{user.id}', '{tier}'], event: 'channel.subscribe', text: '{user} {user.id} {event.tier}' },
  { variant: 'new', tokens: ['{user}', '{user.id}', '{tier}'], event: 'channel.subscribe', text: '{user} {user.id} {event.tier}' },
  { variant: 'resub', tokens: ['{user}', '{user.id}', '{tier}', '{months}', '{streak}', '{message}'], event: 'channel.subscription.message', text: '{user} {user.id} {event.tier} {event.months} {event.streak} {event.message}' },
  { variant: 'gift', tokens: ['{user}', '{user.id}', '{tier}', '{count}', '{total}'], event: 'channel.subscription.gift', text: '{user} {user.id} {event.tier} {event.amount} {event.total}' }
];

type TriggerVariables = { type: FlowNode['type']; config?: FlowNode['config']; tokens: string[]; tier?: string; event?: string; text: string };

const triggerVariables: TriggerVariables[] = [
  ...subscriptions.map(({ variant, ...entry }) => {
    const config: FlowNode['config'] = variant === undefined ? {} : { variant };
    return { ...entry, type: 'twitch.sub', config, tier: '1000' };
  }),
  {
    type: 'twitch.sub_gift', event: 'channel.subscription.gift', tier: '1000',
    tokens: ['{user}', '{user.id}', '{tier}', '{count}', '{total}'], text: '{user} {user.id} {event.tier} {event.amount} {event.total}'
  },
  {
    type: 'twitch.cheer', event: 'channel.cheer',
    tokens: ['{user}', '{user.id}', '{bits}', '{message}'], text: '{user} {user.id} {event.amount} {event.message}'
  },
  {
    type: 'twitch.reward', config: { reward: 'reward-1' },
    tokens: ['{user}', '{user.id}', '{input}', '{reward}', '{cost}'], text: '{user} {user.id} {event.input} {event.reward} {event.cost}'
  },
  {
    type: 'twitch.unban_request', event: 'channel.unban_request.create',
    tokens: ['{user}', '{user.id}', '{text}', '{event.id}'], text: '{user} {user.id} {event.text} {event.id}'
  },
  { type: 'twitch.channel_update', event: 'channel.update', tokens: ['{title}', '{game}'], text: '{event.title} {event.category}' },
  { type: 'twitch.ad_break', event: 'channel.ad_break.begin', tokens: ['{seconds}'], text: '{event.seconds}' },
  { type: 'schedule.manual', tokens: ['{args}'], text: '{args}' },
  { type: 'schedule.timer', tokens: [], text: 'Hello' },
  { type: 'twitch.chat.word', config: { word: 'bagel' }, tokens: ['{user}', '{user.id}', '{args}'], text: '{user} {user.id} {args}' }
];

test.each(triggerVariables)('$type $config offers tokens that save', ({ type, config, tokens, tier, event, text }) => {
  const flow = newFlow('Trigger variables');
  const root = addNode(flow, type, { x: 0, y: 0 });
  if (config) root.config = config;
  const wait = addNode(flow, 'reply.wait', { x: 0, y: 0 }, { from: root.id, port: 'next' });
  const chat = addNode(flow, 'reply.chat', { x: 0, y: 0 }, { from: wait.id, port: 'next' });
  const variables = variablesFor(flow, chat.id);
  chat.config.message = tokens.join(' ') || 'Hello';
  const result = flowToDefinition(flow);
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  expect({
    tokens: variables.map((variable) => variable.token),
    tier: variables.find((variable) => variable.token === '{tier}')?.sample,
    trigger: result.value.trigger.params?.kind,
    chat: result.value.actions![1]!.params!.text,
    issues: validateDefinition(result.value)
  }).toEqual({ tokens, tier, trigger: event, chat: text, issues: [] });
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
