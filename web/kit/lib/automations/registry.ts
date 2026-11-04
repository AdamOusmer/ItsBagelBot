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
import { SIMPLE_ACTIONS } from './actions';
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

const NESTED: Table = {
  if: registerNested<IfParams>(
    { condition: 'typed', then: 'actions', else: 'actions' },
    checkIf,
    (p) => [p.then, p.else],
  ),
  random_pick: registerNested<RandomPickParams>({ branches: 'branches' }, checkRandomPick, (p) =>
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
