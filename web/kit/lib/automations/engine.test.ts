// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { stepRequirement, type RequirementTrigger, type StepRequirement } from './index';
import { PERM_LEVELS } from './values';

const command = { type: 'command' };
const follow = { type: 'event', kind: 'channel.follow' };
const reward = { type: 'reward' };
const unbanRequest = { type: 'event', kind: 'channel.unban_request.create' };
const mod = { ...command, perm: PERM_LEVELS.indexOf('mod') };
const leadMod = { ...command, perm: PERM_LEVELS.indexOf('lead_mod') };

test.each([
  { type: 'chat', trigger: command, expected: null },
  { type: 'constructor', trigger: command, expected: null },
  { type: 'clear_chat', trigger: command, expected: { kind: 'role', level: 'mod' } },
  { type: 'clear_chat', trigger: mod, expected: null },
  { type: 'clear_chat', trigger: follow, expected: null },
  { type: 'raid', trigger: mod, expected: { kind: 'role', level: 'lead_mod' } },
  { type: 'raid', trigger: leadMod, expected: null },
  { type: 'unban_resolve', trigger: command, expected: { kind: 'trigger', trigger: 'unban_request' } },
  { type: 'unban_resolve', trigger: follow, expected: { kind: 'trigger', trigger: 'unban_request' } },
  { type: 'unban_resolve', trigger: unbanRequest, expected: null },
  { type: 'delete_message', trigger: follow, expected: { kind: 'trigger', trigger: 'command' } },
  { type: 'delete_message', trigger: command, expected: { kind: 'role', level: 'mod' } },
  { type: 'redemption', trigger: command, expected: { kind: 'trigger', trigger: 'reward' } },
  { type: 'redemption', trigger: reward, expected: null }
] satisfies Array<{ type: string; trigger: RequirementTrigger; expected: StepRequirement | null }>)(
  '$type with $trigger needs $expected', ({ type, trigger, expected }) => {
    expect(stepRequirement(type, trigger)).toEqual(expected);
  }
);
