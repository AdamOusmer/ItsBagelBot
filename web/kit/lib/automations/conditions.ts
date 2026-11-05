// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Chk } from './check';
import { register, type Table, type Wide } from './engine';
import { COMPARE_OPS } from './compare';
import { CODE, MAX_LIST_ENTRIES, MAX_PATTERN_LENGTH, MAX_TEMPLATE_LENGTH, RECIPE_NAME_PATTERN } from './limits';
import { isCounterChar } from './text';
import { CMP_OPS, PERM_LEVELS } from './values';
import { checkRegex } from './triggers';
import type {
  ArgRegexCondition,
  CategoryCondition,
  ChanceCondition,
  CompareCondition,
  CooldownCondition,
  CounterCondition,
  LiveCondition,
  PermCondition,
  PointsCondition,
  UsersCondition,
  VarCondition,
} from './types';

const LOGIN_PATTERN = /^[a-z0-9_]{1,25}$/;
const USER_ID_PATTERN = /^[0-9]{1,19}$/;
const MAX_CATEGORY_ENTRIES = 50;
const MAX_CATEGORY_LENGTH = 100;
const MAX_COUNTER_NAME = 64;

export function counterName(c: Chk, field: string, name: string): void {
  if (c.text(field, name, 1, MAX_COUNTER_NAME)) c.chars(field, name, isCounterChar);
}

function checkUsers(c: Chk, p: Wide<UsersCondition>): void {
  c.atLeastOne(p.allow.length + p.deny.length + p.allow_ids.length + p.deny_ids.length > 0);
  const each = (f: string, s: string) => c.format(f, s, LOGIN_PATTERN);
  c.stringList('allow', p.allow, MAX_LIST_ENTRIES, each);
  c.stringList('deny', p.deny, MAX_LIST_ENTRIES, each);
  const eachId = (f: string, s: string) => checkUserId(c, f, s);
  c.stringList('allow_ids', p.allow_ids, MAX_LIST_ENTRIES, eachId);
  c.stringList('deny_ids', p.deny_ids, MAX_LIST_ENTRIES, eachId);
}

function checkUserId(c: Chk, field: string, s: string): void {
  c.format(field, s, USER_ID_PATTERN);
  if (/^0+$/.test(s)) c.add(field, CODE.outOfRange);
}

function checkCategory(c: Chk, p: Wide<CategoryCondition>): void {
  c.atLeastOne(p.allow.length + p.deny.length > 0);
  const each = (f: string, s: string) => c.text(f, s, 1, MAX_CATEGORY_LENGTH);
  c.stringList('allow', p.allow, MAX_CATEGORY_ENTRIES, each);
  c.stringList('deny', p.deny, MAX_CATEGORY_ENTRIES, each);
}

function checkCooldown(c: Chk, p: Wide<CooldownCondition>): void {
  c.inRange('global_s', p.global_s, 0, 86400);
  c.inRange('user_s', p.user_s, 0, 86400);
  c.atLeastOne(p.global_s > 0 || p.user_s > 0);
}

function checkCounter(c: Chk, p: Wide<CounterCondition>): void {
  counterName(c, 'name', p.name);
  c.oneOf('op', p.op, CMP_OPS);
}

function checkCompare(c: Chk, p: Wide<CompareCondition>): void {
  c.template('left', p.left);
  c.oneOf('op', p.op, COMPARE_OPS);
  c.text('right', p.right, p.op === 'contains' ? 1 : 0, MAX_TEMPLATE_LENGTH);
}

export const VAR_SCOPES = ['channel', 'viewer'];
const VAR_OPS = [...CMP_OPS, 'contains', 'exists'];

export function checkVarIdentity(c: Chk, p: { scope: string; name: string }): void {
  c.oneOf('scope', p.scope, VAR_SCOPES);
  c.format('name', p.name, RECIPE_NAME_PATTERN);
}

function checkVar(c: Chk, p: Wide<VarCondition>): void {
  checkVarIdentity(c, p);
  c.oneOf('op', p.op, VAR_OPS);
  c.text('value', p.value, 0, MAX_TEMPLATE_LENGTH);
}

export const CONDITIONS: Table = {
  perm: register<Wide<PermCondition>>({ level: 'string' }, (c, p) => c.oneOf('level', p.level, PERM_LEVELS)),
  users: register<Wide<UsersCondition>>(
    { allow: 'strings', deny: 'strings', allow_ids: 'strings', deny_ids: 'strings' },
    checkUsers,
  ),
  live: register<Wide<LiveCondition>>({ state: 'string' }, (c, p) => c.oneOf('state', p.state, ['live', 'offline'])),
  cooldown: register<Wide<CooldownCondition>>({ global_s: 'int', user_s: 'int' }, checkCooldown),
  chance: register<Wide<ChanceCondition>>({ percent: 'int' }, (c, p) => c.inRange('percent', p.percent, 1, 100)),
  counter: register<Wide<CounterCondition>>({ name: 'string', op: 'string', value: 'int' }, checkCounter),
  points: register<Wide<PointsCondition>>({ op: 'string', value: 'int' }, (c, p) => c.oneOf('op', p.op, CMP_OPS)),
  arg_regex: register<Wide<ArgRegexCondition>>({ pattern: 'string' }, (c, p) =>
    checkRegex(c, 'pattern', p.pattern, MAX_PATTERN_LENGTH),
  ),
  category: register<Wide<CategoryCondition>>({ allow: 'strings', deny: 'strings' }, checkCategory),
  compare: register<Wide<CompareCondition>>({ left: 'string', op: 'string', right: 'string' }, checkCompare),
  var: register<Wide<VarCondition>>({ scope: 'string', name: 'string', op: 'string', value: 'string' }, checkVar),
};
