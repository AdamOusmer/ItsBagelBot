// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Chk } from './check';
import { CONDITIONS } from './conditions';
import {
  actionRegistry,
  actions,
  type ActionNode,
  conditionRegistry,
  type Entry,
  registerNested,
  type Table,
  triggerRegistry,
  type TypedNode,
  typed,
} from './engine';
import { checkHttpGetFields, checkHttpPostFields, SIMPLE_ACTIONS } from './actions';
import { CODE } from './limits';
import { trimSpace } from './text';
import { TRIGGERS } from './triggers';

interface IfParams {
  condition: TypedNode;
  then: ActionNode[];
  else: ActionNode[];
}

interface Branch {
  weight: number;
  actions: ActionNode[];
}

interface RandomPickParams {
  branches: Branch[];
}

function checkIf(c: Chk, p: IfParams): void {
  typed(c.sub('condition'), conditionRegistry, p.condition);
  actions(c.sub('then').nest(), p.then);
  actions(c.sub('else').nest(), p.else);
}

function checkRandomPick(c: Chk, p: RandomPickParams): void {
  if (!c.count('branches', p.branches.length, 2, 5)) return;
  p.branches.forEach((b, i) => {
    const at = c.sub('branches').elem(i);
    at.inRange('weight', b.weight, 1, 100);
    actions(at.sub('actions').nest(), b.actions);
  });
}

function checkParallel(c: Chk, p: RandomPickParams): void {
  if (!c.count('branches', p.branches.length, 2, 10)) return;
  p.branches.forEach((b, i) => {
    actions(c.sub('branches').elem(i).sub('actions').nest(), b.actions);
  });
}

interface LoopParams {
  times: string;
  actions: ActionNode[];
}

const LOOP_TIMES_TEXT = /^[+-]?\d+$/;

function checkLoop(c: Chk, p: LoopParams): void {
  c.template('times', p.times);
  const fixed = p.times !== '' && !p.times.includes('{');
  if (fixed && !LOOP_TIMES_TEXT.test(trimSpace(p.times))) c.add('times', CODE.invalidFormat);
  actions(c.sub('actions').nest(), p.actions);
}

interface ApiPaths {
  on_ok: ActionNode[];
  on_failure: ActionNode[];
}

interface HttpPostParams extends ApiPaths {
  url: string;
  body: string;
  save_as: string;
}

interface HttpGetParams extends ApiPaths {
  url: string;
  json_path: string[];
  save_as: string;
}

function checkApiPaths(c: Chk, p: ApiPaths): void {
  actions(c.sub('on_ok').nest(), p.on_ok);
  actions(c.sub('on_failure').nest(), p.on_failure);
}

const apiLists = (p: ApiPaths): ActionNode[][] => [p.on_ok, p.on_failure];

const asEffect = (entry: Entry): Entry => ({ ...entry, effect: true });

const NESTED: Table = {
  loop: registerNested<LoopParams>({ times: 'string', actions: 'actions' }, checkLoop, (p) => [p.actions]),
  http_get: asEffect(
    registerNested<HttpGetParams>(
      { url: 'string', json_path: 'strings', save_as: 'string', on_ok: 'actions', on_failure: 'actions' },
      (c, p) => {
        checkHttpGetFields(c, p);
        checkApiPaths(c, p);
      },
      apiLists,
    ),
  ),
  http_post: asEffect(
    registerNested<HttpPostParams>(
      { url: 'string', body: 'string', save_as: 'string', on_ok: 'actions', on_failure: 'actions' },
      (c, p) => {
        checkHttpPostFields(c, p);
        checkApiPaths(c, p);
      },
      apiLists,
    ),
  ),
  if: registerNested<IfParams>(
    { condition: 'typed', then: 'actions', else: 'actions' },
    checkIf,
    (p) => [p.then, p.else],
  ),
  random_pick: registerNested<RandomPickParams>({ branches: 'branches' }, checkRandomPick, (p) =>
    p.branches.map((b) => b.actions),
  ),
  parallel: registerNested<RandomPickParams>({ branches: 'branches' }, checkParallel, (p) =>
    p.branches.map((b) => b.actions),
  ),
};

function fill(target: Map<string, Entry>, table: Table): void {
  for (const [name, entry] of Object.entries(table)) target.set(name, entry);
}

export function fillRegistries(): void {
  fill(triggerRegistry, TRIGGERS);
  fill(conditionRegistry, CONDITIONS);
  fill(actionRegistry, { ...SIMPLE_ACTIONS, ...NESTED });
}
