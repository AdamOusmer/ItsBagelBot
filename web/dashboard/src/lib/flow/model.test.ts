// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { flowToDefinition, validateDefinition } from '@bagel/kit/automations';
import { newFlow, variablesFor } from './model';
import { addNode } from './ops';

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
