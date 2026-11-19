// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  eventTokensToBuilder, FLOW_ISSUE, LOOP_TYPE, RANDOM_PATHS, RANDOM_TYPE, apiPorts, str, templateFields, triggerKind,
  type Flow, type FlowConfig, type FlowEdge, type FlowMeta, type FlowNode, type FlowResult
} from './flow-graph-model';
import { ACTION_READERS, API_READERS, conditionToNode } from './flow-graph-readers';
import { outgoing, priorCounters } from './flow-graph-topology';
import { triggerToNode } from './flow-graph-triggers';
import { streamInfoRun } from './stream-flow';
import type { Action, Condition, Definition, Issue } from './types';

type Tail = { from: string; port: string };

type Building = { nodes: FlowNode[]; edges: FlowEdge[]; issues: Issue[] };

function addNode(b: Building, type: string, config: FlowConfig, tails: Tail[]): string {
  const id = `n${b.nodes.length + 1}`;
  b.nodes.push({ id, type, x: 0, y: 0, config });
  for (const tail of tails) b.edges.push({ id: `e${b.edges.length + 1}`, from: tail.from, port: tail.port, to: id });
  return id;
}

const placedConfig = (action: Action, config: FlowConfig): FlowConfig => (action.enabled === false ? { ...config, disabled: true } : config);

function placeGate(b: Building, condition: Condition, path: string, tails: Tail[]): string | null {
  const node = conditionToNode(condition);
  if (node) return addNode(b, node.type, node.config, tails);
  b.issues.push({ path, code: FLOW_ISSUE.unsupportedCondition });
  return null;
}

function placeIf(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  const params = action.params ?? {};
  const gate = placeGate(b, params.condition as Condition, `${path}.condition`, tails);
  if (!gate) return tails;
  const yes = placeActions(b, (params.then ?? []) as Action[], `${path}.then`, [{ from: gate, port: 'yes' }]);
  const no = placeActions(b, (params.else ?? []) as Action[], `${path}.else`, [{ from: gate, port: 'no' }]);
  return [...yes, ...no];
}

function placeParallel(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  const branches = (action.params?.branches ?? []) as { actions?: Action[] }[];
  return branches.flatMap((branch, i) => placeActions(b, branch.actions ?? [], `${path}.params.branches[${i}].actions`, tails));
}

function placeLoop(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  const params = action.params ?? {};
  const id = addNode(b, LOOP_TYPE, placedConfig(action, { times: str(params.times) }), tails);
  placeActions(b, (params.actions ?? []) as Action[], `${path}.params.actions`, [{ from: id, port: 'body' }]);
  return [{ from: id, port: 'done' }];
}

function placeApi(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  const params = action.params ?? {};
  const node = API_READERS[action.type]!(params);
  const id = addNode(b, node.type, placedConfig(action, node.config), tails);
  const [okPort, failurePort] = apiPorts(action.type);
  const worked = placeActions(b, (params.on_ok ?? []) as Action[], `${path}.params.on_ok`, [{ from: id, port: okPort }]);
  const failed = placeActions(b, (params.on_failure ?? []) as Action[], `${path}.params.on_failure`, [{ from: id, port: failurePort }]);
  return [...worked, ...failed];
}

function placeRandom(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  const branches = (action.params?.branches ?? []) as { weight: number; actions?: Action[] }[];
  const ports = RANDOM_PATHS.slice(0, branches.length);
  const config = Object.fromEntries(ports.map(({ weightKey }, i) => [weightKey, branches[i]!.weight] as const));
  const id = addNode(b, RANDOM_TYPE, placedConfig(action, { ...config, paths: String(branches.length) }), tails);
  return ports.flatMap(({ id: port }, i) => placeActions(b, branches[i]!.actions ?? [], `${path}.params.branches[${i}].actions`, [{ from: id, port }]));
}

const CONTROL_READERS = { if: placeIf, parallel: placeParallel, loop: placeLoop, random_pick: placeRandom } as const;

function placeAction(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  if (disabledControlFlow(action)) {
    b.issues.push({ path, code: FLOW_ISSUE.unsupportedDisabledControlFlow });
    return tails;
  }
  if (Object.hasOwn(CONTROL_READERS, action.type)) return CONTROL_READERS[action.type as keyof typeof CONTROL_READERS](b, action, path, tails);
  if (Object.hasOwn(API_READERS, action.type)) return placeApi(b, action, path, tails);
  const node = Object.hasOwn(ACTION_READERS, action.type) ? ACTION_READERS[action.type]!(action.params ?? {}, action) : null;
  if (!node) {
    b.issues.push({ path, code: FLOW_ISSUE.unsupportedAction });
    return tails;
  }
  return [{ from: addNode(b, node.type, placedConfig(action, node.config), tails), port: 'next' }];
}

function disabledControlFlow(action: Action): boolean {
  return action.enabled === false && ['if', 'parallel'].includes(action.type);
}

function placeActions(b: Building, actions: Action[], path: string, tails: Tail[]): Tail[] {
  let index = 0;
  while (index < actions.length) {
    const action = actions[index]!;
    const streamInfo = streamInfoRun(actions, index);
    tails = streamInfo
      ? [{ from: addNode(b, 'reply.stream_info', placedConfig(action, streamInfo.config), tails), port: 'next' }]
      : placeAction(b, action, `${path}[${index}]`, tails);
    index = streamInfo?.end ?? index + 1;
  }
  return tails;
}

function placeConditions(b: Building, conditions: Condition[], tails: Tail[]): Tail[] {
  return conditions.reduce<Tail[]>((open, condition, i) => {
    const gate = placeGate(b, condition, `conditions[${i}]`, open);
    return gate ? [{ from: gate, port: 'yes' }] : open;
  }, tails);
}

type CounterScan = { kind: string; counters: Map<string, string[]> };

function raiseTemplate(scan: CounterScan, node: FlowNode, field: string): string {
  const counters = scan.counters.get(node.id) ?? [];
  return eventTokensToBuilder(scan.kind, str(node.config[field]), counters.length > 0);
}

function raiseMessages(b: Building, kind: string): void {
  const flow = { nodes: b.nodes, edges: b.edges } as Flow;
  const scan: CounterScan = { kind, counters: priorCounters({ byId: new Map(b.nodes.map((node) => [node.id, node])), outs: outgoing(flow) }) };
  for (const node of b.nodes) {
    for (const field of templateFields(node)) {
      node.config = { ...node.config, [field]: raiseTemplate(scan, node, field) };
    }
  }
}

export function definitionToFlow(definition: Definition, meta: FlowMeta): FlowResult<Flow> {
  const trigger = triggerToNode(definition.trigger);
  if (!trigger) return { ok: false, issues: [{ path: 'trigger', code: FLOW_ISSUE.unsupportedTrigger }] };
  const b: Building = { nodes: [], edges: [], issues: [] };
  const root = addNode(b, trigger.type, trigger.config, []);
  const gated = placeConditions(b, definition.conditions ?? [], [{ from: root, port: 'next' }]);
  placeActions(b, definition.actions ?? [], 'actions', gated);
  if (b.issues.length > 0) return { ok: false, issues: b.issues };
  raiseMessages(b, triggerKind(definition.trigger));
  const queue = definition.queue ? { queue: definition.queue } : {};
  return { ok: true, value: { ...meta, ...queue, nodes: b.nodes, edges: b.edges } };
}
