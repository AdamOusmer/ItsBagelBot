// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  activeRandomPaths, amountUnit, apiPorts, armsOf, awaitingUser, BARE_TOKEN, CHECK_PORTS, EVENT_ONLY_TOKENS, EVENT_TOKENS,
  FLOW_ISSUE, hasRedemptionStep, isConditionType, LOOP_PORTS, LOOP_TYPE, nodePath, num,
  RANDOM_TYPE, randomPathWeight, recipeName, str, templateFields, triggerKind, viewerList,
  type Flow, type FlowConfig, type FlowEdge, type FlowNode, type FlowResult
} from './flow-graph-model';
import { outgoing, priorCounters, reachable, shapeIssues, triggerNode } from './flow-graph-topology';
import { TRIGGER_WRITERS } from './flow-graph-triggers';
import { ACTION_WRITERS, API_WRITERS, CONDITION_WRITERS } from './flow-graph-writers';
import { CODE, MAX_DELAY_MS, VERSION } from './limits';
import { invalidLiteralTags } from './stream-flow';
import type { Action, Condition, Definition, Issue, Params, Trigger } from './types';

type Lowering = {
  trigger: Trigger;
  byId: Map<string, FlowNode>;
  outs: Map<string, FlowEdge[]>;
  counters: Map<string, string[]>;
  visited: Set<string>;
  issues: Issue[];
  owners: Map<object, string>;
};

function targets(ctx: Lowering, id: string, port: string): string[] {
  return (ctx.outs.get(id) ?? []).filter((e) => e.port === port).map((e) => e.to);
}

function lowerCountToken(ctx: Lowering, counters: string[]): string | null {
  const eventCount = EVENT_TOKENS[triggerKind(ctx.trigger)]?.count;
  if (eventCount) return counters.length > 0 ? null : `{event.${eventCount}}`;
  if (counters.length > 1) return null;
  if (counters.length === 1) return `{counter:${counters[0]}}`;
  return ctx.trigger.type === 'command' ? '{count}' : null;
}

function lowerToken(ctx: Lowering, name: string, counters: string[]): string | null {
  if (name === 'count') return lowerCountToken(ctx, counters);
  const field = EVENT_TOKENS[triggerKind(ctx.trigger)]?.[name];
  if (field) return `{event.${field}}`;
  return EVENT_ONLY_TOKENS.has(name) ? null : `{${name}}`;
}

function lowerTemplate(ctx: Lowering, node: FlowNode, field: string): string {
  const counters = ctx.counters.get(node.id) ?? [];
  return str(node.config[field]).replace(BARE_TOKEN, (token, name: string) => {
    const lowered = lowerToken(ctx, name, counters);
    if (lowered !== null) return lowered;
    ctx.issues.push({ path: `${nodePath(node.id)}.${field}.${name}`, code: FLOW_ISSUE.unmappableToken });
    return token;
  });
}

function loweredConfig(ctx: Lowering, node: FlowNode): FlowConfig {
  const lowered: FlowConfig = { ...node.config };
  for (const field of templateFields(node)) lowered[field] = lowerTemplate(ctx, node, field);
  return lowered;
}

type Stops = ReadonlySet<string>;

const NO_STOPS: Stops = new Set<string>();

const reachOf = (ctx: Lowering, id: string) => reachable(ctx.outs, id);

function earliest(ctx: Lowering, shared: string[]): string[] {
  const later = new Set(shared.flatMap((id) => (ctx.outs.get(id) ?? []).map((edge) => edge.to)));
  return shared.filter((id) => !later.has(id));
}

function commonJoins(ctx: Lowering, entries: string[]): string[] {
  if (new Set(entries).size < 2) return [];
  const sets = entries.map((id) => reachOf(ctx, id));
  return earliest(ctx, [...sets[0]!].filter((id) => sets.every((s) => s.has(id))));
}

function armJoins(ctx: Lowering, first: string[], second: string[]): string[] {
  if (first.length === 0 || second.length === 0) return [];
  const other = new Set(second.flatMap((id) => [...reachOf(ctx, id)]));
  const joins = earliest(ctx, [...new Set(first.flatMap((id) => [...reachOf(ctx, id)]))].filter((id) => other.has(id)));
  const everyPathMeets = [...first, ...second].every((id) => joins.some((join) => reachOf(ctx, id).has(join)));
  return everyPathMeets ? joins : [];
}

const extended = (stop: Stops, joins: string[]): Stops => (joins.length > 0 ? new Set([...stop, ...joins]) : stop);

function visit(ctx: Lowering, id: string): boolean {
  if (!ctx.visited.has(id)) {
    ctx.visited.add(id);
    return true;
  }
  ctx.issues.push({ path: nodePath(id), code: FLOW_ISSUE.unstructuredMerge });
  return false;
}

function userListResolved(config: FlowConfig): boolean {
  if (awaitingUser(config)) return true;
  const viewers = viewerList(config);
  return viewers.length > 0 && viewers.every(({ id }) => id !== '');
}

function writeCondition(ctx: Lowering, node: FlowNode): Condition {
  const write = CONDITION_WRITERS[node.type];
  if (!write) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.unsupportedNode });
    return { type: node.type };
  }
  if (node.type === 'cond.user' && !userListResolved(node.config)) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.userNotResolved });
    return { type: 'users' };
  }
  return owned(ctx, node, write(loweredConfig(ctx, node)));
}

function owned<T extends object>(ctx: Lowering, node: FlowNode, written: T): T {
  ctx.owners.set(written, node.id);
  return written;
}

function rangeIssue(ctx: Lowering, node: FlowNode, field: 'amount' | 'per'): void {
  ctx.issues.push({ path: `${nodePath(node.id)}.config.${field}`, code: CODE.outOfRange });
}

function perBelowMinimum(config: FlowConfig): boolean {
  return String(config.per ?? '').trim() !== '' && num(config.per) < 1;
}

function actionConfig(ctx: Lowering, node: FlowNode): FlowConfig {
  const config = loweredConfig(ctx, node);
  switch (node.type) {
    case 'reply.points':
      if (config.change && num(config.amount) < 0) rangeIssue(ctx, node, 'amount');
      return config;
    case 'reply.countdown_add':
    case 'reply.cooldown_add':
      if (perBelowMinimum(config)) rangeIssue(ctx, node, 'per');
      return amountUnit(ctx.trigger) ? config : { ...config, per: '' };
    default:
      return config;
  }
}

function writeAction(ctx: Lowering, node: FlowNode): Action[] {
  const write = ACTION_WRITERS[node.type];
  if (!write) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.unsupportedNode });
    return [{ type: node.type }];
  }
  const written = write(actionConfig(ctx, node));
  const actions = Array.isArray(written) ? written : [written];
  if (actions.length === 0) ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.nothingToChange });
  if (node.type === 'reply.stream_info' && invalidLiteralTags(node.config.tags)) {
    ctx.issues.push({ path: `${nodePath(node.id)}.tags`, code: FLOW_ISSUE.invalidTags });
  }
  return actions.map((action) => ownAction(ctx, node, action));
}

function ownAction(ctx: Lowering, node: FlowNode, action: Action): Action {
  if (action.type === 'delay' && num(action.params?.ms) > MAX_DELAY_MS) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.waitTooLong });
  }
  return owned(ctx, node, withEnabled(node, action));
}

const nestedLists = (action: Action): Action[][] => armsOf(action).map((arm) => arm.list);

const waits = (list: Action[]): boolean => list.some((a) => a.type === 'delay' || nestedLists(a).some(waits));

const forkPauses = (paths: Action[][]) => paths.length > 1 && paths.some(waits);

function fork(ctx: Lowering, paths: Action[][], source: FlowNode['id'] | undefined): Action[] {
  if (paths.length < 2) return paths[0] ?? [];
  const branches = paths.map((actions) => ({ weight: 1, actions }));
  if (source !== undefined) ctx.owners.set(branches, source);
  return [{ type: 'parallel', params: { branches } }];
}

function lowerRest(ctx: Lowering, joins: string[], stop: Stops): Action[] {
  const open = joins.filter((id) => !stop.has(id));
  return open.length > 0 ? lowerEntries(ctx, open, stop) : [];
}

function lowerEntries(ctx: Lowering, entries: string[], stop: Stops, source?: FlowNode['id']): Action[] {
  entries = entries.filter((id) => !stop.has(id));
  if (entries.length === 1) return lowerFrom(ctx, entries[0]!, stop);
  const joins = commonJoins(ctx, entries);
  const bound = extended(stop, joins);
  const paths = entries.map((id) => lowerFrom(ctx, id, bound)).filter((path) => path.length > 0);
  const anchor = joins[0] ?? [...stop][0];
  if (anchor && forkPauses(paths)) ctx.issues.push({ path: nodePath(anchor), code: FLOW_ISSUE.unstructuredMerge });
  return [...fork(ctx, paths, source), ...lowerRest(ctx, joins, stop)];
}

function lowerFrom(ctx: Lowering, id: string, stop: Stops): Action[] {
  const actions: Action[] = [];
  let entries = [id];
  let source = id;
  while (entries.length === 1) {
    const current = entries[0]!;
    if (stop.has(current) || !visit(ctx, current)) return actions;
    const node = ctx.byId.get(current)!;
    const control = controlActions(ctx, node, stop);
    if (control) return actions.concat(control);
    actions.push(...writeAction(ctx, node));
    source = current;
    entries = targets(ctx, current, 'next');
  }
  return actions.concat(lowerEntries(ctx, entries, stop, source));
}

const CONTROL_WRITERS: Readonly<Record<string, typeof lowerLoop>> = {
  [LOOP_TYPE]: lowerLoop,
  [RANDOM_TYPE]: lowerRandom
};

function controlActions(ctx: Lowering, node: FlowNode, stop: Stops): Action[] | null {
  if (isConditionType(node.type)) return lowerBranch(ctx, node, stop);
  if (Object.hasOwn(CONTROL_WRITERS, node.type)) return CONTROL_WRITERS[node.type]!(ctx, node, stop);
  return Object.hasOwn(API_WRITERS, node.type) ? lowerApi(ctx, node, stop) : null;
}

type Arms = { first: Action[]; second: Action[]; rest: Action[] };

function lowerArms(ctx: Lowering, node: FlowNode, ports: readonly [string, string], stop: Stops): Arms {
  const first = targets(ctx, node.id, ports[0]);
  const second = targets(ctx, node.id, ports[1]);
  const joins = armJoins(ctx, first, second);
  const bound = extended(stop, joins);
  return { first: lowerEntries(ctx, first, bound, node.id), second: lowerEntries(ctx, second, bound, node.id), rest: lowerRest(ctx, joins, stop) };
}

function lowerBranch(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const params: Params = { condition: writeCondition(ctx, node) };
  const { first, second, rest } = lowerArms(ctx, node, CHECK_PORTS, stop);
  if (first.length > 0) params.then = first;
  if (second.length > 0) params.else = second;
  return [owned(ctx, node, { type: 'if', params }), ...rest];
}

const withEnabled = (node: FlowNode, action: Action): Action => (node.config.disabled === true ? { ...action, enabled: false } : action);

function randomJoins(ctx: Lowering, paths: string[][]): string[] {
  if (paths.some((path) => path.length === 0)) return [];
  const reached = paths.flat().map((id) => reachOf(ctx, id));
  return earliest(ctx, [...reached[0]!].filter((id) => reached.every((set) => set.has(id))));
}

function lowerRandom(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const ports = activeRandomPaths(node.config);
  const paths = ports.map(({ id }) => targets(ctx, node.id, id));
  const joins = randomJoins(ctx, paths);
  const bound = extended(stop, joins);
  const branches = ports.map(({ weightKey }, i) => ({
    weight: randomPathWeight(node.config, weightKey), actions: lowerEntries(ctx, paths[i]!, bound, node.id)
  }));
  return [owned(ctx, node, withEnabled(node, { type: 'random_pick', params: { branches } })), ...lowerRest(ctx, joins, stop)];
}

function lowerLoop(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const times = str(loweredConfig(ctx, node).times);
  const { first, second, rest } = lowerArms(ctx, node, LOOP_PORTS, stop);
  const params: Params = first.length > 0 ? { times, actions: first } : { times };
  return [owned(ctx, node, withEnabled(node, { type: 'loop', params })), ...second, ...rest];
}

function lowerApi(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const action = API_WRITERS[node.type]!(loweredConfig(ctx, node));
  const { first, second, rest } = lowerArms(ctx, node, apiPorts(action.type), stop);
  const params = { ...action.params };
  if (first.length > 0) params.on_ok = first;
  if (second.length > 0) params.on_failure = second;
  return [owned(ctx, node, withEnabled(node, { ...action, params })), ...rest];
}

function leadingGate(ctx: Lowering, entries: string[]): FlowNode | null {
  if (entries.length !== 1) return null;
  const node = ctx.byId.get(entries[0]!)!;
  const gated = isConditionType(node.type) && targets(ctx, node.id, 'no').length === 0;
  return gated ? node : null;
}

function lowerRoot(ctx: Lowering, trigger: FlowNode): { conditions: Condition[]; actions: Action[] } {
  const conditions: Condition[] = [];
  let source = trigger.id;
  let entries = targets(ctx, trigger.id, 'next');
  for (let gate = leadingGate(ctx, entries); gate; gate = leadingGate(ctx, entries)) {
    visit(ctx, gate.id);
    conditions.push(writeCondition(ctx, gate));
    source = gate.id;
    entries = targets(ctx, gate.id, 'yes');
  }
  return { conditions, actions: lowerEntries(ctx, entries, NO_STOPS, source) };
}

function assembled(flow: Flow, trigger: Trigger, conditions: Condition[], actions: Action[]): Definition {
  const definition: Definition = { version: VERSION, trigger };
  const queue = recipeName(flow.queue);
  if (queue) definition.queue = queue;
  if (conditions.length > 0) definition.conditions = conditions;
  if (actions.length > 0) definition.actions = actions;
  return definition;
}

type Lowered = { result: FlowResult<Definition>; owners: Map<object, string> };

export function lowerFlow(flow: Flow): Lowered {
  const owners = new Map<object, string>();
  const found = triggerNode(flow);
  if (!found.ok) return { result: found, owners };
  const trigger = found.value;
  const settles = trigger.type === 'twitch.reward' && hasRedemptionStep(flow.nodes);
  const config = settles ? { ...trigger.config, onRedeem: 'leave' } : trigger.config;
  const written = TRIGGER_WRITERS[trigger.type]?.(config);
  const ctx: Lowering = {
    trigger: written ?? { type: '' },
    byId: new Map(flow.nodes.map((n) => [n.id, n])),
    outs: outgoing(flow),
    counters: new Map(),
    visited: new Set([trigger.id]),
    issues: [],
    owners
  };
  const shape = shapeIssues(flow, trigger, ctx);
  if (shape.length > 0) return { result: { ok: false, issues: shape }, owners };
  if (!written) return { result: { ok: false, issues: [{ path: nodePath(trigger.id), code: FLOW_ISSUE.unsupportedNode }] }, owners };
  ctx.counters = priorCounters(ctx);
  const { conditions, actions } = lowerRoot(ctx, trigger);
  if (ctx.issues.length > 0) return { result: { ok: false, issues: ctx.issues }, owners };
  return { result: { ok: true, value: assembled(flow, written, conditions, actions) }, owners };
}

export function flowToDefinition(flow: Flow): FlowResult<Definition> {
  return lowerFlow(flow).result;
}
