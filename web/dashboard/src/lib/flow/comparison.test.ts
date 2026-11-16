// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { defaultComparison } from './comparison';
import { insertNode, replaceTrigger } from './ops';
import { newFlow } from './model';

test.each([
  { trigger: 'twitch.cheer', left: '{bits}', right: '1000', op: 'gte' },
  { trigger: 'twitch.raid', left: '{viewers}', right: '10', op: 'gte' },
  { trigger: 'twitch.chat.command', left: '{message}', right: 'hello', op: 'eq' },
  { trigger: 'twitch.chat.command', left: '{user}', right: 'Lotti', op: 'eq' },
  { trigger: 'twitch.reward', left: '{input}', right: '42', op: 'eq' },
  { trigger: 'twitch.follow', left: '42', right: 'hello', op: 'eq' },
  { trigger: 'twitch.follow', left: '5', right: '10', op: 'gte' },
  { trigger: 'twitch.follow', left: '', right: '', op: 'gte' }
])('$left compared with $right defaults to $op', ({ trigger, left, right, op }) => {
  const flow = newFlow('Compare');
  const at = { x: 0, y: 0 };
  const root = replaceTrigger(flow, trigger);
  const node = insertNode(flow, { type: 'cond.compare', at, placement: { kind: 'port', from: root.id, port: 'next', mode: 'insert' } });
  node.config = { left, right };
  expect(defaultComparison(flow, node)).toBe(op);
});
