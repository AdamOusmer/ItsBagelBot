// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { staticText } from '../../../../kit/lib/i18n/static';
import { stepKeywords, stepLabel, stepSummary } from './copy';
import { describe } from './describe';
import { english } from './english.testing';
import type { Translate } from './messages';
import { NODE_SPECS, specFor, type FlowKind, type FlowNode } from './model';
import { searchSteps } from './search';
import { STREAM_STEPS } from './stream-steps';

const types = (query: Parameters<typeof searchSteps>[1], kinds?: readonly FlowKind[]) => searchSteps(english, query, kinds).map((spec) => spec.type);

test.each([
  ['wait', 'reply.wait'],
  ['delay', 'reply.wait'],
  ['wiat', 'reply.wait'],
  ['waitt', 'reply.wait'],
  ['walt', 'reply.wait'],
  ['send message', 'reply.chat'],
  ['say', 'reply.chat'],
  ['loop', 'reply.loop'],
  ['add points', 'reply.points'],
  ['ligth', 'reply.lights'],
  ['  SEND, méssage! ', 'reply.chat']
])('%s ranks %s first', (query, type) => {
  expect(types(query)[0]).toBe(type);
});

test.each([
  ['api', ['reply.http_get', 'reply.http_post']],
  ['if', NODE_SPECS.filter((spec) => spec.kind === 'condition').map((spec) => spec.type)],
  ['mod', ['reply.mod', 'reply.chat_mode', 'cond.role', 'twitch.unban_request', 'reply.unban_resolve']],
  ['modules', []],
  ['zzzz', []],
  ['send zzzz', []],
  ['wa', ['reply.wait']],
  ['wi', ['reply.counter', 'reply.prediction_end']],
  ['waaitt', []]
])('%s returns only the matching steps in source order', (query, expected) => {
  expect(types(query)).toEqual(expected);
});

test.each([
  ['reply', 'condition'],
  ['condition', 'reply'],
  ['trigger'],
  []
] satisfies FlowKind[][])('an empty query follows kind, group and source order: %j', (...kinds) => {
  const expected = kinds.flatMap((kind) => ['twitch', 'modules', 'basics'].flatMap((group) =>
    NODE_SPECS.filter((spec) => spec.kind === kind && spec.group === group).map((spec) => spec.type)));
  expect(types('  ', kinds)).toEqual(expected);
});

test.each([
  ['api', ['condition'], []],
  ['wait', ['reply'], ['reply.wait']],
  ['if', ['trigger'], []]
] satisfies [string, FlowKind[], string[]][])('%s stays within %j', (query, kinds, expected) => {
  expect(types(query, kinds)).toEqual(expected);
});

test('default empty search puts actions before conditions', () => {
  expect([...new Set(searchSteps(english, '').map((spec) => spec.kind))]).toEqual(['trigger', 'reply', 'condition']);
});

test.each([
  ['beacon', {
    'flows.step.reply.chat.label': 'Beacon message',
    'flows.step.reply.wait.label': 'A beacon',
    'flows.step.reply.points.keywords': 'beacon',
    'flows.step.trigger.follow.summary': 'A beacon'
  }, ['reply.chat', 'reply.wait', 'reply.points', 'twitch.follow']],
  ['beacon signal', {
    'flows.step.reply.chat.label': 'Beacon signal',
    'flows.step.reply.wait.label': 'A beacon signal',
    'flows.step.reply.points.label': 'Beacon',
    'flows.step.reply.points.keywords': 'signal',
    'flows.step.trigger.follow.label': 'Beacon',
    'flows.step.trigger.follow.summary': 'signal',
    'flows.step.reply.counter.keywords': 'beacon signal'
  }, ['reply.chat', 'reply.wait', 'reply.points', 'twitch.follow', 'reply.counter']]
] satisfies [string, Record<string, string>, string[]][])('%s ranks label prefix, label words, keywords and summary in order', (query, phrases, expected) => {
  const messages: Record<string, string> = phrases;
  const translate: Translate = (key) => messages[key] ?? '';
  expect(searchSteps(translate, query).map((spec) => spec.type)).toEqual(expected);
});

test.each(['en', 'de', 'es', 'fr', 'pt-br', 'ru'])('%s has keywords for every step and distinct action summaries', (locale) => {
  const translate: Translate = (key) => staticText(locale, key);
  const missing = NODE_SPECS.filter((spec) => !stepKeywords(translate, spec));
  const repeated = STREAM_STEPS.filter((spec) => spec.kind === 'reply').filter((spec) => stepLabel(translate, spec) === stepSummary(translate, spec));
  expect({ missing: missing.map((spec) => spec.type), repeated: repeated.map((spec) => spec.type) }).toEqual({ missing: [], repeated: [] });
});

test.each([
  ['de', 'verzogern', 'reply.wait'],
  ['es', 'lámpara', 'reply.lights'],
  ['fr', 'répéter', 'reply.loop'],
  ['pt-br', 'moedas', 'reply.points'],
  ['ru', 'пауза', 'reply.wait'],
  ['unknown', 'delay', 'reply.wait']
])('%s finds %s with locale or English fallback', (locale, query, type) => {
  expect(searchSteps((key) => staticText(locale, key), query)[0]?.type).toBe(type);
});

test('missing keywords become empty instead of searchable raw keys', () => {
  const translate: Translate = (key) => key;
  expect(stepKeywords(translate, specFor('reply.chat')!)).toBe('');
  expect(searchSteps((key) => key.endsWith('.keywords') ? key : '', 'keywords')).toEqual([]);
});

test.each([
  ['cond.compare', { left: '42', right: '10' }, '42 is at least 10'],
  ['cond.compare', { left: '42', right: '10', op: 'lt' }, '42 is less than 10'],
  ['twitch.sub', { variant: 'any' }, 'Someone gets a new or gifted sub'],
  ['twitch.sub', {}, 'Someone gets a new or gifted sub']
] satisfies [string, FlowNode['config'], string][])('%s describes displayed defaults truthfully: %j', (type, config, expected) => {
  expect(describe(english, { id: 'step', type, config, x: 0, y: 0 })).toBe(expected);
});
