// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { flowToDefinition, validateDefinition } from '@bagel/kit/automations';
import { NODE_SPECS, STEP_PLACEMENT, newFlow, outputsFor, type FlowNode } from './model';
import { addNode } from './ops.testing';
import { addStep } from './editor';
import en from '../../../../../locales/en/console/flows/board.json';
import de from '../../../../../locales/de/console/flows/board.json';
import es from '../../../../../locales/es/console/flows/board.json';
import fr from '../../../../../locales/fr/console/flows/board.json';
import pt from '../../../../../locales/pt-br/console/flows/board.json';
import ru from '../../../../../locales/ru/console/flows/board.json';

const at = { x: 0, y: 0 };
const placement = { kind: 'canvas' } as const;
const defaults = {
  'twitch.chat.command': {},
  'twitch.chat.word': {},
  'twitch.follow': {},
  'twitch.sub': { variant: 'any' },
  'twitch.cheer': {},
  'twitch.raid': {},
  'twitch.unban_request': {},
  'twitch.stream.online': {},
  'twitch.stream.offline': {},
  'twitch.reward': {},
  'countdown.finished': {},
  'cond.role': { level: 'mod' },
  'cond.user': {},
  'cond.cooldown': { seconds: 30 },
  'cond.compare': { op: 'gte' },
  'cond.chance': { percent: 50 },
  'cond.var': { scope: 'channel', op: 'exists' },
  'reply.chat': { message: 'Thanks {user}!' },
  'reply.shoutout': { target: '{user}' },
  'reply.announce': { text: 'Thanks {user}!', color: 'primary' },
  'reply.reply': { text: 'Thanks!' },
  'reply.timeout': { target: '{user}', seconds: 600 },
  'reply.mod': { on: true },
  'reply.vip': { on: true },
  'reply.raid': {},
  'reply.raid_cancel': {},
  'reply.whisper': {},
  'reply.unban_resolve': { status: 'deny' },
  'reply.ad_snooze': {},
  'reply.branded_content': {},
  'reply.chat_mode': { mode: 'slow', on: true },
  'reply.clear_chat': {},
  'reply.poll': { seconds: 60 },
  'reply.poll_end': { status: 'terminate' },
  'reply.prediction': { seconds: 120 },
  'reply.prediction_end': { status: 'lock' },
  'reply.points': { amount: 10 },
  'reply.counter': {},
  'reply.reward_update': { setEnabled: 'on' },
  'reply.countdown_start': { seconds: 3_600 },
  'reply.countdown_add': { seconds: 60 },
  'reply.cooldown_add': { seconds: 60 },
  'reply.lights': { mode: 'color', liveOnly: true },
  'reply.wait': { seconds: 5 },
  'reply.loop': { times: '3' },
  'reply.var_set': { scope: 'channel' },
  'reply.var_add': { scope: 'channel', amount: 1 },
  'reply.var_delete': { scope: 'channel' },
  'reply.text': { op: 'upper' },
  'reply.math': {},
  'reply.http_get': {},
  'reply.http_post': {}
} satisfies Record<keyof typeof STEP_PLACEMENT, FlowNode['config']>;

test.each(NODE_SPECS)('$type gets its expected config when added', (spec) => {
  const flow = newFlow('Defaults');
  if (spec.kind !== 'trigger') addNode(flow, 'twitch.follow', at);
  const { node } = addStep(flow, { type: spec.type, at, placement, chatMessage: en.defaultChatMessage });
  expect(node.config).toEqual(defaults[spec.type as keyof typeof defaults]);
});

const invalidDefaults = new Set(['out_of_range', 'invalid_enum', 'too_short']);
const requiredSetup: Record<string, FlowNode['config']> = {
  'cond.user': { userId: '42' },
  'reply.poll': { choices: 'First\nSecond' },
  'reply.prediction': { outcomes: 'First\nSecond' }
};

test.each(NODE_SPECS)('$type lowers and validates its defaults with a broadcaster check', (spec) => {
  const flow = newFlow('Validate defaults');
  const isTrigger = spec.kind === 'trigger';
  const trigger = addNode(flow, isTrigger ? spec.type : 'twitch.follow', at);
  const gate = addNode(flow, 'cond.role', at, { from: trigger.id, port: 'next' });
  gate.config.level = 'broadcaster';
  const node = isTrigger ? gate : addNode(flow, spec.type, at, { from: gate.id, port: 'yes' });
  Object.assign(node.config, requiredSetup[spec.type]);
  const port = isTrigger ? 'yes' : outputsFor(spec)[0].id;
  addNode(flow, 'reply.chat', at, { from: node.id, port }).config.message = 'Saved';

  const lowered = flowToDefinition(flow);
  if (!lowered.ok) throw new Error(JSON.stringify(lowered.issues));
  expect(validateDefinition(lowered.value).filter((issue) => invalidDefaults.has(issue.code))).toEqual([]);
});

test.each([
  { trigger: 'twitch.chat.command', target: '{touser}', message: en.defaultChatMessage },
  { trigger: 'twitch.chat.word', target: '{user}', message: en.defaultChatMessage },
  { trigger: 'twitch.raid', target: '{user}', message: en.defaultChatMessage },
  { trigger: 'twitch.follow', target: '{user}', message: en.defaultChatMessage },
  { trigger: 'twitch.sub', target: '{user}', message: en.defaultChatMessage },
  { trigger: 'twitch.reward', target: '{user}', message: en.defaultChatMessage },
  { trigger: 'twitch.stream.online', target: '', message: en.defaultPlainMessage },
  { trigger: 'twitch.stream.offline', target: '', message: en.defaultPlainMessage },
  { trigger: 'countdown.finished', target: '', message: en.defaultPlainMessage },
  { trigger: null, target: '', message: '' }
])('contextual defaults follow $trigger', ({ trigger, target, message }) => {
  const flow = newFlow('Contextual defaults');
  if (trigger) addNode(flow, trigger, at);
  const types = ['reply.shoutout', 'reply.timeout', 'reply.announce', 'reply.reply', 'reply.chat'];
  const configs = types.map((type) => addStep(flow, { type, at, placement, chatMessage: en.defaultChatMessage }).node.config);
  expect(configs).toEqual([
    { target },
    { ...defaults['reply.timeout'], target },
    { ...defaults['reply.announce'], text: message || en.defaultPlainMessage },
    defaults['reply.reply'],
    { message }
  ]);
});

test.each(Object.entries({ en, de, es, fr, 'pt-br': pt, ru }))('chat defaults use the %s translation for each trigger', (_locale, catalog) => {
  const messages = ['twitch.follow', 'twitch.stream.offline'].map((trigger) => {
    const flow = newFlow('Localized defaults');
    addNode(flow, trigger, at);
    return addStep(flow, {
      type: 'reply.chat', at, placement, chatMessage: catalog.defaultChatMessage, plainMessage: catalog.defaultPlainMessage
    }).node.config.message;
  });
  expect(messages).toEqual([catalog.defaultChatMessage, catalog.defaultPlainMessage]);
});
