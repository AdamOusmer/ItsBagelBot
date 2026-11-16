// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { flowToDefinition, locateIssues, validateDefinition } from '@bagel/kit/automations';
import { NODE_SPECS, newFlow, outputsFor, type FieldSpec, type NodeSpec } from './model';
import { addNode, insertNode } from './ops';
import { addStep } from './editor';
import en from '../../../../../locales/en/console/flows/board.json';
import de from '../../../../../locales/de/console/flows/board.json';
import es from '../../../../../locales/es/console/flows/board.json';
import fr from '../../../../../locales/fr/console/flows/board.json';
import pt from '../../../../../locales/pt-br/console/flows/board.json';
import ru from '../../../../../locales/ru/console/flows/board.json';

const at = { x: 0, y: 0 };

const prefills = {
  'reply.shoutout': { target: '{user}' },
  'reply.announce': { text: 'Thanks {user}!', color: 'primary' },
  'reply.reply': { text: 'Thanks {user}!' },
  'reply.timeout': { target: '{user}', seconds: 60 },
  'reply.wait': { seconds: 5 },
  'cond.cooldown': { seconds: 30 },
  'reply.chat': { message: 'Thanks {user}!' },
  'cond.chance': { percent: 50 }
};

const paramNames: Record<string, string> = { message: 'text', setEnabled: 'enabled', variant: 'kind' };

function defaultFields(spec: NodeSpec): string[] {
  return spec.fields.filter((field) => field.type === 'select' || field.type === 'toggle').map((field) => paramNames[field.key] ?? field.key);
}

function displayedDefault(field: FieldSpec): [string, string | boolean][] {
  if (field.type === 'select') return [[field.key, field.options[0]]];
  if (field.type === 'toggle' && field.defaultOn) return [[field.key, true]];
  return [];
}

test.each(NODE_SPECS)('$type saves its displayed defaults without edits', (spec) => {
  const flow = newFlow('Defaults');
  const root = spec.kind === 'trigger' ? null : addNode(flow, 'twitch.follow', at);
  const placement = root ? { kind: 'port' as const, from: root.id, port: 'next', mode: 'insert' as const } : { kind: 'canvas' as const };
  const { node } = addStep(flow, { type: spec.type, at, placement, chatMessage: en.defaultChatMessage });
  const expected = { ...Object.fromEntries(spec.fields.flatMap(displayedDefault)), ...prefills[spec.type as keyof typeof prefills] };
  expect(node.config).toEqual(expected);
  addNode(flow, 'reply.chat', at, { from: node.id, port: outputsFor(spec)[0].id }).config.message = 'Saved';
  const result = flowToDefinition(flow);
  if (!result.ok) {
    expect({ type: spec.type, fields: defaultFields(spec), codes: result.issues.map((issue) => issue.code) }).toEqual({ type: 'cond.user', fields: [], codes: ['user_not_resolved'] });
    return;
  }
  const missing = locateIssues(flow, validateDefinition(result.value)).filter((issue) => issue.code === 'required');
  const fields = defaultFields(spec);
  const missingDefaults = missing.filter((issue) => issue.path.startsWith(`nodes.${node.id}`) && fields.includes(issue.path.split('.').at(-1)!));
  const requiredChange = spec.type === 'reply.reward_update' ? [{ path: `nodes.${node.id}.params.enabled`, code: 'required' }] : [];
  expect(missingDefaults).toEqual(requiredChange);
});

test.each(Object.entries(prefills))('%s prefill passes definition validation', (type) => {
  const flow = newFlow('Prefills');
  const root = addNode(flow, 'twitch.follow', at);
  const node = insertNode(flow, { type, at, placement: { kind: 'port', from: root.id, port: 'next', mode: 'insert' }, chatMessage: en.defaultChatMessage });
  const result = flowToDefinition(flow);
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  expect({ config: node.config, issues: validateDefinition(result.value) }).toEqual({ config: prefills[type as keyof typeof prefills], issues: [] });
});

test.each(['twitch.follow', 'twitch.sub', 'twitch.reward', 'twitch.stream.online', 'countdown.finished', null])(
  'chat uses viewer tokens only with viewer trigger %s',
  (type) => {
    const flow = newFlow('Message');
    if (type) addNode(flow, type, at);
    const chat = insertNode(flow, { type: 'reply.chat', at, placement: { kind: 'canvas' }, chatMessage: en.defaultChatMessage });
    const viewer = ['twitch.follow', 'twitch.sub', 'twitch.reward'].includes(type ?? '');
    expect(chat.config.message).toBe(viewer ? 'Thanks {user}!' : '');
  }
);

test.each(Object.entries({ en, de, es, fr, 'pt-br': pt, ru }))('editor uses the %s chat prefill', (_locale, catalog) => {
  const flow = newFlow('Translated');
  addNode(flow, 'twitch.follow', at);
  const { node } = addStep(flow, { type: 'reply.chat', at, placement: { kind: 'canvas' }, chatMessage: catalog.defaultChatMessage });
  expect(node.config.message).toBe(catalog.defaultChatMessage);
});
