// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { MOD, moduleDef } from '../catalog';
import { ALERT_KEYS, alertDefinition, classifyFlow, commandDefinition, plainAlertLine, simpleFlowPreview, type SimpleCommand } from './flow-layer';
import { MAX_PREVIEW_BYTES } from './limits';
import { validateDefinition } from './validate';
import type { Definition } from './types';

const chat = { type: 'chat', params: { text: 'hi' } };
const command = (conditions?: Definition['conditions'], actions: Definition['actions'] = [chat]): Definition => ({
  version: 1,
  trigger: { type: 'command', params: { name: '!hug' } },
  conditions,
  actions
});
const event = (kind: string, extra: Partial<Definition> = {}, filters?: Record<string, string>): Definition => ({
  version: 1,
  trigger: { type: 'event', params: filters ? { kind, filters } : { kind } },
  actions: [chat],
  ...extra
});

const simple: SimpleCommand = { name: 'hug', aliases: ['cuddle'], response: 'hi', perm: 'mod', cooldown: 30, userCooldown: 5 };

describe('simpleFlowPreview', () => {
  test('an alert is a trigger box and a reply box in a row', () => {
    expect(JSON.parse(simpleFlowPreview(alertDefinition('follow', 'x')))).toEqual({
      n: [
        [0, 0, 4, 0],
        [15, 0, 4, 2]
      ],
      e: [[0, 1, 0]]
    });
  });

  test('gates sit between them and step down through their yes port', () => {
    expect(JSON.parse(simpleFlowPreview(commandDefinition(simple)))).toEqual({
      n: [
        [0, 0, 4, 0],
        [15, 0, 7, 1],
        [31, 2, 7, 1],
        [46, 5, 7, 1],
        [61, 7, 4, 2]
      ],
      e: [
        [0, 1, 0],
        [1, 2, 0],
        [2, 3, 0],
        [3, 4, 0]
      ]
    });
  });

  test('it fits the preview limit at the largest simple shape', () => {
    expect(simpleFlowPreview(commandDefinition(simple)).length).toBeLessThan(MAX_PREVIEW_BYTES);
  });
});

describe('classifyFlow', () => {
  test('a command with one chat line and role or cooldown gates is a command', () => {
    const definition = command([
      { type: 'perm', params: { level: 'vip' } },
      { type: 'cooldown', params: { global_s: 10 } },
      { type: 'cooldown', params: { user_s: 4 } }
    ]);
    expect(classifyFlow(definition)).toEqual({
      layer: 'command',
      command: { name: 'hug', aliases: [], response: 'hi', perm: 'vip', cooldown: 10, userCooldown: 4 }
    });
  });

  test('a stored command reads back to the same row fields', () => {
    const definition = commandDefinition(simple);
    expect(validateDefinition(definition)).toEqual([]);
    expect(classifyFlow(definition)).toEqual({ layer: 'command', command: simple });
  });

  test.each([
    ['another condition', command([{ type: 'chance', params: { percent: 50 } }])],
    ['two roles', command([{ type: 'perm', params: { level: 'vip' } }, { type: 'perm', params: { level: 'mod' } }])],
    ['two global cooldowns', command([{ type: 'cooldown', params: { global_s: 1 } }, { type: 'cooldown', params: { global_s: 2 } }])],
    ['two lines', command(undefined, [chat, chat])],
    ['another action', command(undefined, [{ type: 'points_add', params: { amount: 1 } }])],
    ['a disabled line', command(undefined, [{ ...chat, enabled: false }])],
    ['a nested if', command(undefined, [{ type: 'if', params: { condition: { type: 'chance', params: { percent: 1 } }, then: [chat] } }])],
    ['regex match', { version: 1, trigger: { type: 'command', params: { name: 'h.g', match: 'regex' } }, actions: [chat] }],
    ['exact match', { version: 1, trigger: { type: 'command', params: { name: '!hug', match: 'exact' } }, actions: [chat] }],
    ['a sub message source', { version: 1, trigger: { type: 'command', params: { name: '!hug', sources: ['sub_message'] } }, actions: [chat] }],
    ['no prefix', { version: 1, trigger: { type: 'command', params: { name: 'hug' } }, actions: [chat] }]
  ] satisfies [string, Definition][])('a command with %s is an automation', (_label, definition) => {
    expect(classifyFlow(definition)).toEqual({ layer: 'automation' });
  });

  test.each(ALERT_KEYS)('a bare %s line is the alert', (alert) => {
    const definition = alertDefinition(alert, 'thanks');
    expect(validateDefinition(definition)).toEqual([]);
    expect(classifyFlow(definition)).toEqual({ layer: 'alert', alert, message: 'thanks' });
  });

  test.each([
    ['a threshold', event('channel.cheer', {}, { amount: '100' })],
    ['a condition', event('channel.follow', { conditions: [{ type: 'chance', params: { percent: 50 } }] })],
    ['two lines', event('channel.follow', { actions: [chat, chat] })],
    ['a kind without an alert', event('stream.online')],
    ['the resub kind', event('channel.subscription.message')]
  ] satisfies [string, Definition][])('an event with %s is an automation', (_label, definition) => {
    expect(classifyFlow(definition)).toEqual({ layer: 'automation' });
  });

  test.each([
    ['cheer', 'thanks {user} for {bits}', 'thanks {user} for {event.amount}'],
    ['raid', '{user} brought {viewers}', '{user} brought {event.viewers}'],
    ['gift', '{user} gifted {count} at {tier}', '{user} gifted {event.amount} at {event.tier}'],
    ['sub', 'tier {tier}', 'tier {event.tier}']
  ] as const)('%s alert tokens are stored in the module form and read in the engine form', (alert, stored, engine) => {
    const definition = alertDefinition(alert, stored);
    expect(definition.actions?.[0]?.params?.text).toBe(engine);
    expect(classifyFlow(definition)).toEqual({ layer: 'alert', alert, message: stored });
  });

  test.each(['{event.message}', 'live on {game}', 'up for {uptime}', '{args}', '{var:deaths}', '{viewers}'])(
    'a cheer line using %s stays an automation, an alert would print it literally',
    (text) => {
      const definition = event('channel.cheer', { actions: [{ type: 'chat', params: { text } }] });
      expect(classifyFlow(definition)).toEqual({ layer: 'automation' });
    }
  );

  test.each(ALERT_KEYS)('the catalog default %s line reads as that alert, not an automation', (alert) => {
    const line = moduleDef(MOD.alerts)!.replies.find((reply) => reply.key === alert)!.defaultMessage!;
    expect(line).toContain('{alerts:');
    expect(classifyFlow(alertDefinition(alert, line))).toEqual({ layer: 'alert', alert, message: plainAlertLine(line) });
  });

  test('a definition already carrying namespaced tokens reads as the alert too', () => {
    const definition = event('channel.cheer', { actions: [{ type: 'chat', params: { text: '{alerts:user} sent {alerts:bits}' } }] });
    expect(classifyFlow(definition)).toEqual({ layer: 'alert', alert: 'cheer', message: '{user} sent {bits}' });
  });

  test('pure tokens render in an alert line', () => {
    const definition = event('channel.follow', { actions: [{ type: 'chat', params: { text: 'hi {user} {choice:a,b} {random:1-6}' } }] });
    expect(classifyFlow(definition)).toMatchObject({ layer: 'alert', alert: 'follow' });
  });

  test('other triggers are automations', () => {
    expect(classifyFlow({ version: 1, trigger: { type: 'reward', params: { reward_id: 'r' } }, actions: [chat] })).toEqual({ layer: 'automation' });
  });
});
