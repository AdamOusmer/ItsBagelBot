// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { COMPARE_OPS } from './compare';
import { SETUP_LOGIN } from './flow-setup';
import { CODE, MAX_DELAY_MS, VERSION } from './limits';
import type { Action, Condition, Definition, Issue, Params, Trigger } from './types';

export type FlowConfig = Record<string, string | number | boolean>;
export type FlowNode = { id: string; type: string; x: number; y: number; config: FlowConfig };
export type FlowEdge = { id: string; from: string; port: string; to: string };
export type Flow = { id: string; name: string; enabled: boolean; nodes: FlowNode[]; edges: FlowEdge[]; queue?: string };
export type FlowMeta = Pick<Flow, 'id' | 'name' | 'enabled'>;

export type FlowResult<T> = { ok: true; value: T } | { ok: false; issues: Issue[] };

export const FLOW_ISSUE = {
  noTrigger: 'no_trigger',
  multipleTriggers: 'multiple_triggers',
  cycle: 'cycle',
  unconnectedNode: 'unconnected_node',
  unstructuredMerge: 'unstructured_merge',
  unsupportedNode: 'unsupported_node',
  unsupportedTrigger: 'unsupported_trigger',
  unsupportedCondition: 'unsupported_condition',
  unsupportedAction: 'unsupported_action',
  waitTooLong: 'wait_too_long',
  unmappableToken: 'unmappable_token',
  userNotResolved: 'user_not_resolved'
} as const;

const REWARD_KIND = 'channel.channel_points_custom_reward_redemption.add';

const EVENT_TOKENS: Record<string, Record<string, string>> = {
  'channel.cheer': { bits: 'amount' },
  'channel.raid': { viewers: 'viewers' },
  'channel.subscribe': { tier: 'tier' },
  'channel.subscription.message': { tier: 'tier', months: 'months' },
  'channel.subscription.gift': { tier: 'tier', count: 'amount' },
  [REWARD_KIND]: { input: 'input' }
};

const EVENT_ONLY_TOKENS = new Set(['bits', 'viewers', 'tier', 'months', 'input']);

const BARE_TOKEN = /\{([a-z]+)\}/g;
const EVENT_TOKEN = /\{event\.([a-z_]+)\}/g;

export function builderTokensToEvent(kind: string, text: string): string {
  const tokens = EVENT_TOKENS[kind] ?? {};
  return text.replace(BARE_TOKEN, (token, name: string) => (tokens[name] ? `{event.${tokens[name]}}` : token));
}

export function eventTokensToBuilder(kind: string, text: string, keepCount = false): string {
  const tokens = Object.entries(EVENT_TOKENS[kind] ?? {}).filter(([name]) => !keepCount || name !== 'count');
  return text.replace(EVENT_TOKEN, (token, field: string) => {
    const match = tokens.find(([, mapped]) => mapped === field);
    return match ? `{${match[0]}}` : token;
  });
}

const SUB_KINDS: Record<string, string> = {
  any: 'channel.subscribe',
  new: 'channel.subscribe',
  resub: 'channel.subscription.message',
  gift: 'channel.subscription.gift'
};

const THRESHOLD_FILTERS: Record<string, { key: string; filter: string }> = {
  'channel.cheer': { key: 'minBits', filter: 'amount' },
  'channel.raid': { key: 'minViewers', filter: 'viewers' }
};

const FIXED_EVENT_NODES: Record<string, string> = {
  'twitch.follow': 'channel.follow',
  'twitch.stream.online': 'stream.online'
};

const isTriggerType = (type: string) => type.startsWith('twitch.');
const isConditionType = (type: string) => type.startsWith('cond.');

const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const num = (v: unknown): number => (typeof v === 'number' ? v : Number(v) || 0);

function eventTrigger(kind: string, filters?: Record<string, string>): Trigger {
  return { type: 'event', params: filters ? { kind, filters } : { kind } };
}

function thresholdTrigger(kind: string, config: FlowConfig): Trigger {
  const threshold = THRESHOLD_FILTERS[kind]!;
  const min = num(config[threshold.key]);
  return min > 1 ? eventTrigger(kind, { [threshold.filter]: String(min) }) : eventTrigger(kind);
}

function commandTrigger(config: FlowConfig): Trigger {
  const name = str(config.trigger).trim();
  const aliases = str(config.aliases).split(',').filter(Boolean);
  const params: Params = { name: name.startsWith('!') ? name : `!${name}` };
  if (aliases.length > 0) params.aliases = aliases;
  return { type: 'command', params };
}

// RE2's \b is ASCII only, so it would never close after a word ending in é or punctuation.
const WORD_START = '(?:^|[^\\p{L}\\p{N}_])';
const WORD_END = '(?:$|[^\\p{L}\\p{N}_])';

// A backslash stays \x5c because the validator reads \1 anywhere in a pattern as a backreference.
const escapeWord = (word: string) => word.replace(/[\\.*+?^$()[\]{}|]/g, (char) => (char === '\\' ? '\\x5c' : `\\${char}`));
const unescapeWord = (body: string) => body.replace(/\\x5c|\\(.)/g, (_, char?: string) => char ?? '\\');

function wordPattern(word: string): string {
  return word === '' ? '' : `${WORD_START}${escapeWord(word)}${WORD_END}`;
}

function wordTrigger(config: FlowConfig): Trigger {
  return { type: 'command', params: { name: wordPattern(str(config.word).trim()), match: 'regex' } };
}

function subTrigger(config: FlowConfig): Trigger | null {
  const variant = str(config.variant) || 'any';
  const kind = SUB_KINDS[variant];
  if (!kind) return null;
  return variant === 'new' ? eventTrigger(kind, { is_gift: 'false' }) : eventTrigger(kind);
}

const TRIGGER_WRITERS: Record<string, (config: FlowConfig) => Trigger | null> = {
  'twitch.chat.command': commandTrigger,
  'twitch.chat.word': wordTrigger,
  'twitch.follow': () => eventTrigger('channel.follow'),
  'twitch.stream.online': () => eventTrigger('stream.online'),
  'twitch.sub': subTrigger,
  'twitch.cheer': (config) => thresholdTrigger('channel.cheer', config),
  'twitch.raid': (config) => thresholdTrigger('channel.raid', config),
  'twitch.reward': (config) => ({ type: 'reward', params: { reward_id: str(config.reward) } })
};

const TEMPLATE_FIELDS: Record<string, string[]> = {
  'reply.chat': ['message'],
  'cond.compare': ['left', 'right'],
  'cond.var': ['value'],
  'reply.var_set': ['value'],
  'reply.text': ['input', 'find', 'replaceWith', 'start', 'count', 'separator', 'pick', 'needle'],
  'reply.math': ['expression'],
  'reply.loop': ['times'],
  'reply.http_get': ['url'],
  'reply.http_post': ['url', 'body']
};

const recipeName = (v: unknown) => str(v).trim().toLowerCase();

const varIdentity = (config: FlowConfig): Params => ({ scope: str(config.scope) || 'channel', name: recipeName(config.name) });

const TEXT_ARG_FIELDS = new Map<string, readonly string[]>([
  ['replace', ['find', 'replaceWith']],
  ['substring', ['start', 'count']],
  ['split_pick', ['separator', 'pick']],
  ['contains', ['needle']]
]);

function textAction(config: FlowConfig): Action {
  const op = str(config.op) || 'upper';
  const [first, second] = TEXT_ARG_FIELDS.get(op) ?? [];
  const arg = (key: string | undefined) => (key ? str(config[key]) : '');
  return { type: 'text', params: { op, input: str(config.input), arg1: arg(first), arg2: arg(second), save_as: recipeName(config.saveAs) } };
}

const REWARD_ENABLED = new Map<string, boolean | null>([
  ['keep', null],
  ['on', true],
  ['off', false]
]);

function rewardUpdateAction(config: FlowConfig): Action {
  const cost = config.cost === undefined || config.cost === '' ? null : num(config.cost);
  const enabled = REWARD_ENABLED.get(str(config.setEnabled)) ?? null;
  return { type: 'reward_update', params: { reward_id: str(config.reward), enabled, cost, reset_on_stream_end: config.reset === true } };
}

function withOptional(params: Params, optional: Params): Params {
  for (const [key, value] of Object.entries(optional)) if (value) params[key] = value;
  return params;
}

const JSON_PATH_SEPARATOR = '.';

function jsonPathOf(config: FlowConfig): string[] | undefined {
  const segments = str(config.jsonPath).trim().split(JSON_PATH_SEPARATOR).filter(Boolean);
  return segments.length > 0 ? segments : undefined;
}

const API_WRITERS: Record<string, (config: FlowConfig) => Action> = {
  'reply.http_get': (config) => ({
    type: 'http_get',
    params: withOptional({ url: str(config.url) }, { json_path: jsonPathOf(config), save_as: recipeName(config.saveAs) })
  }),
  'reply.http_post': (config) => ({
    type: 'http_post',
    params: withOptional({ url: str(config.url) }, { body: str(config.body), save_as: recipeName(config.saveAs) })
  })
};

const API_PORTS = ['ok', 'failure'] as const;
const LOOP_TYPE = 'reply.loop';
const LOOP_PORTS = ['body', 'done'] as const;

const DEFAULT_LIGHT_COLOR = '#ffffff';

function lightParams(config: FlowConfig): Params {
  const params: Params = { device: str(config.device), sku: str(config.sku) };
  if (str(config.deviceName)) params.device_name = str(config.deviceName);
  return params;
}

function sceneParams(config: FlowConfig): Params {
  const picked = str(config.scene) !== '';
  const keep = (key: string) => (picked ? str(config[key]) : '');
  const scene: Params = { scene_id: str(config.scene), scene_type: keep('sceneType'), scene_instance: keep('sceneInstance'), scene_value: keep('sceneValue') };
  if (keep('sceneName')) scene.scene_name = keep('sceneName');
  return scene;
}

function lightsAction(config: FlowConfig): Action {
  const params = lightParams(config);
  if (config.mode === 'scene') return { type: 'lights', params: { ...params, mode: 'scene', ...sceneParams(config) } };
  return { type: 'lights', params: { ...params, mode: 'color', color: str(config.color) || DEFAULT_LIGHT_COLOR } };
}

const awaitingUser = (config: FlowConfig) => config.login === SETUP_LOGIN;

const CONDITION_WRITERS: Record<string, (config: FlowConfig) => Condition> = {
  'cond.compare': (config) => ({ type: 'compare', params: { left: str(config.left), op: str(config.op) || 'gte', right: str(config.right) } }),
  'cond.role': (config) => ({ type: 'perm', params: { level: str(config.level) } }),
  'cond.chance': (config) => ({ type: 'chance', params: { percent: num(config.percent) } }),
  'cond.var': (config) => {
    const op = str(config.op) || 'eq';
    return { type: 'var', params: { ...varIdentity(config), op, value: op === 'exists' ? '' : str(config.value) } };
  },
  'cond.user': (config) => ({ type: 'users', params: awaitingUser(config) ? { allow: [SETUP_LOGIN] } : { allow_ids: [str(config.userId)] } }),
  'cond.cooldown': (config) => ({
    type: 'cooldown',
    params: { [config.perUser ? 'user_s' : 'global_s']: num(config.seconds) }
  })
};

const ACTION_WRITERS: Record<string, (config: FlowConfig) => Action> = {
  'reply.chat': (config) => ({ type: 'chat', params: { text: str(config.message) } }),
  'reply.lights': lightsAction,
  'reply.points': (config) => ({ type: 'points_add', params: { amount: num(config.amount) } }),
  'reply.counter': (config) => ({ type: 'counter_add', params: { name: str(config.counter), amount: 1 } }),
  'reply.wait': (config) => ({ type: 'delay', params: { ms: num(config.seconds) * 1000 } }),
  'reply.var_set': (config) => ({ type: 'var_set', params: { ...varIdentity(config), value: str(config.value) } }),
  'reply.var_add': (config) => ({ type: 'var_add', params: { ...varIdentity(config), amount: num(config.amount) } }),
  'reply.var_delete': (config) => ({ type: 'var_delete', params: varIdentity(config) }),
  'reply.text': textAction,
  'reply.math': (config) => ({ type: 'math', params: { expression: str(config.expression), save_as: recipeName(config.saveAs) } }),
  'reply.reward_update': rewardUpdateAction
};

type Lowering = {
  nodes: FlowNode[];
  trigger: Trigger;
  byId: Map<string, FlowNode>;
  outs: Map<string, FlowEdge[]>;
  reach: Map<string, Set<string>>;
  visited: Set<string>;
  issues: Issue[];
  owners: Map<object, string>;
};

const nodePath = (id: string) => `nodes.${id}`;

function outgoing(flow: Flow): Map<string, FlowEdge[]> {
  const outs = new Map<string, FlowEdge[]>();
  for (const edge of flow.edges) outs.set(edge.from, [...(outs.get(edge.from) ?? []), edge]);
  return outs;
}

function targets(ctx: Lowering, id: string, port: string): string[] {
  return (ctx.outs.get(id) ?? []).filter((e) => e.port === port).map((e) => e.to);
}

function findCycleNode(outs: Map<string, FlowEdge[]>, start: string): string | null {
  const done = new Set<string>();
  const onPath = new Set<string>();
  const walk = (id: string): string | null => {
    if (onPath.has(id)) return id;
    if (done.has(id)) return null;
    onPath.add(id);
    for (const edge of outs.get(id) ?? []) {
      const hit = walk(edge.to);
      if (hit) return hit;
    }
    onPath.delete(id);
    done.add(id);
    return null;
  };
  return walk(start);
}

function reachable(outs: Map<string, FlowEdge[]>, id: string, memo: Map<string, Set<string>>): Set<string> {
  const known = memo.get(id);
  if (known) return known;
  const set = new Set<string>([id]);
  memo.set(id, set);
  for (const edge of outs.get(id) ?? []) {
    for (const seen of reachable(outs, edge.to, memo)) set.add(seen);
  }
  return set;
}

function priorCounters(nodes: FlowNode[], outs: Map<string, FlowEdge[]>, memo: Map<string, Set<string>>, id: string): string[] {
  const prior = nodes.filter((n) => n.type === 'reply.counter' && n.id !== id && reachable(outs, n.id, memo).has(id));
  return [...new Set(prior.map((n) => str(n.config.counter)))];
}

function triggerKind(trigger: Trigger): string {
  if (trigger.type === 'reward') return REWARD_KIND;
  return trigger.type === 'event' ? str(trigger.params?.kind) : '';
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
  const counters = priorCounters(ctx.nodes, ctx.outs, ctx.reach, node.id);
  return str(node.config[field]).replace(BARE_TOKEN, (token, name: string) => {
    const lowered = lowerToken(ctx, name, counters);
    if (lowered !== null) return lowered;
    ctx.issues.push({ path: `${nodePath(node.id)}.${field}.${name}`, code: FLOW_ISSUE.unmappableToken });
    return token;
  });
}

function loweredConfig(ctx: Lowering, node: FlowNode): FlowConfig {
  const lowered: FlowConfig = { ...node.config };
  for (const field of TEMPLATE_FIELDS[node.type] ?? []) lowered[field] = lowerTemplate(ctx, node, field);
  return lowered;
}

function triggerNode(flow: Flow): FlowResult<FlowNode> {
  const triggers = flow.nodes.filter((n) => isTriggerType(n.type));
  if (triggers.length === 0) return { ok: false, issues: [{ path: 'nodes', code: FLOW_ISSUE.noTrigger }] };
  if (triggers.length > 1) {
    return { ok: false, issues: triggers.map((n) => ({ path: nodePath(n.id), code: FLOW_ISSUE.multipleTriggers })) };
  }
  return { ok: true, value: triggers[0]! };
}

function shapeIssues(flow: Flow, trigger: FlowNode, ctx: Lowering): Issue[] {
  const cycle = findCycleNode(ctx.outs, trigger.id);
  if (cycle) return [{ path: nodePath(cycle), code: FLOW_ISSUE.cycle }];
  const attached = reachable(ctx.outs, trigger.id, ctx.reach);
  const known = new Set(flow.nodes.map((n) => n.id));
  const dangling = flow.edges.filter((e) => !known.has(e.from) || !known.has(e.to));
  return [
    ...flow.nodes.filter((n) => !attached.has(n.id)).map((n) => n.id),
    ...dangling.map((e) => (known.has(e.from) ? e.to : e.from))
  ].map((id) => ({ path: nodePath(id), code: FLOW_ISSUE.unconnectedNode }));
}

type Stops = ReadonlySet<string>;

const NO_STOPS: Stops = new Set<string>();

const reachOf = (ctx: Lowering, id: string) => ctx.reach.get(id) ?? reachable(ctx.outs, id, ctx.reach);

const earliest = (ctx: Lowering, shared: string[]) => shared.filter((id) => !shared.some((other) => other !== id && reachOf(ctx, other).has(id)));

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

function writeCondition(ctx: Lowering, node: FlowNode): Condition {
  const write = CONDITION_WRITERS[node.type];
  if (!write) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.unsupportedNode });
    return { type: node.type };
  }
  const resolved = str(node.config.userId) !== '' || awaitingUser(node.config);
  if (node.type === 'cond.user' && !resolved) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.userNotResolved });
  }
  return owned(ctx, node, write(loweredConfig(ctx, node)));
}

function owned<T extends object>(ctx: Lowering, node: FlowNode, written: T): T {
  ctx.owners.set(written, node.id);
  return written;
}

function writeAction(ctx: Lowering, node: FlowNode): Action {
  const write = ACTION_WRITERS[node.type];
  if (!write) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.unsupportedNode });
    return { type: node.type };
  }
  const action = write(loweredConfig(ctx, node));
  if (action.type === 'delay' && num(action.params?.ms) > MAX_DELAY_MS) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.waitTooLong });
  }
  return owned(ctx, node, node.config.disabled === true ? { ...action, enabled: false } : action);
}

const NESTED_PARAMS = ['then', 'else', 'on_ok', 'on_failure', 'actions'] as const;

type Arm = { key: string; list: Action[] };

function armsOf(action: Action): Arm[] {
  const params = action.params ?? {};
  const named = NESTED_PARAMS.map((key): Arm => ({ key, list: (params[key] ?? []) as Action[] }));
  const branches = (params.branches ?? []) as { actions?: Action[] }[];
  return [...named, ...branches.map((branch, b): Arm => ({ key: `branches[${b}].actions`, list: branch.actions ?? [] }))];
}

const nestedLists = (action: Action): Action[][] => armsOf(action).map((arm) => arm.list);

const waits = (list: Action[]): boolean => list.some((a) => a.type === 'delay' || nestedLists(a).some(waits));

const forkPauses = (paths: Action[][]) => paths.length > 1 && paths.some(waits);

function fork(paths: Action[][]): Action[] {
  if (paths.length < 2) return paths[0] ?? [];
  return [{ type: 'parallel', params: { branches: paths.map((actions) => ({ weight: 1, actions })) } }];
}

function lowerRest(ctx: Lowering, joins: string[], stop: Stops): Action[] {
  const open = joins.filter((id) => !stop.has(id));
  return open.length > 0 ? lowerEntries(ctx, open, stop) : [];
}

function lowerEntries(ctx: Lowering, entries: string[], stop: Stops): Action[] {
  const joins = commonJoins(ctx, entries);
  const bound = extended(stop, joins);
  const paths = entries.map((id) => lowerFrom(ctx, id, bound)).filter((path) => path.length > 0);
  const anchor = joins[0] ?? [...stop][0];
  // A later step would not wait for a path that paused, so it would run before that path finished.
  if (anchor && forkPauses(paths)) ctx.issues.push({ path: nodePath(anchor), code: FLOW_ISSUE.unstructuredMerge });
  return [...fork(paths), ...lowerRest(ctx, joins, stop)];
}

function lowerFrom(ctx: Lowering, id: string, stop: Stops): Action[] {
  if (stop.has(id) || !visit(ctx, id)) return [];
  const node = ctx.byId.get(id)!;
  if (isConditionType(node.type)) return lowerBranch(ctx, node, stop);
  if (node.type === LOOP_TYPE) return lowerLoop(ctx, node, stop);
  if (Object.hasOwn(API_WRITERS, node.type)) return lowerApi(ctx, node, stop);
  return [writeAction(ctx, node), ...lowerEntries(ctx, targets(ctx, id, 'next'), stop)];
}

type Arms = { first: Action[]; second: Action[]; rest: Action[] };

function lowerArms(ctx: Lowering, node: FlowNode, ports: readonly [string, string], stop: Stops): Arms {
  const first = targets(ctx, node.id, ports[0]);
  const second = targets(ctx, node.id, ports[1]);
  const joins = armJoins(ctx, first, second);
  const bound = extended(stop, joins);
  return { first: lowerEntries(ctx, first, bound), second: lowerEntries(ctx, second, bound), rest: lowerRest(ctx, joins, stop) };
}

function lowerBranch(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const params: Params = { condition: writeCondition(ctx, node) };
  const { first, second, rest } = lowerArms(ctx, node, ['yes', 'no'], stop);
  if (first.length > 0) params.then = first;
  if (second.length > 0) params.else = second;
  return [owned(ctx, node, { type: 'if', params }), ...rest];
}

const withEnabled = (node: FlowNode, action: Action): Action => (node.config.disabled === true ? { ...action, enabled: false } : action);

function lowerLoop(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const times = str(loweredConfig(ctx, node).times);
  const { first, second, rest } = lowerArms(ctx, node, LOOP_PORTS, stop);
  const params: Params = first.length > 0 ? { times, actions: first } : { times };
  return [owned(ctx, node, withEnabled(node, { type: 'loop', params })), ...second, ...rest];
}

function lowerApi(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const action = API_WRITERS[node.type]!(loweredConfig(ctx, node));
  const { first, second, rest } = lowerArms(ctx, node, API_PORTS, stop);
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
  let entries = targets(ctx, trigger.id, 'next');
  for (let gate = leadingGate(ctx, entries); gate; gate = leadingGate(ctx, entries)) {
    visit(ctx, gate.id);
    conditions.push(writeCondition(ctx, gate));
    entries = targets(ctx, gate.id, 'yes');
  }
  return { conditions, actions: lowerEntries(ctx, entries, NO_STOPS) };
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

function lowerFlow(flow: Flow): Lowered {
  const owners = new Map<object, string>();
  const found = triggerNode(flow);
  if (!found.ok) return { result: found, owners };
  const trigger = found.value;
  const written = TRIGGER_WRITERS[trigger.type]?.(trigger.config);
  const ctx: Lowering = {
    nodes: flow.nodes,
    trigger: written ?? { type: '' },
    byId: new Map(flow.nodes.map((n) => [n.id, n])),
    outs: outgoing(flow),
    reach: new Map(),
    visited: new Set([trigger.id]),
    issues: [],
    owners
  };
  const shape = shapeIssues(flow, trigger, ctx);
  if (shape.length > 0) return { result: { ok: false, issues: shape }, owners };
  if (!written) return { result: { ok: false, issues: [{ path: nodePath(trigger.id), code: FLOW_ISSUE.unsupportedNode }] }, owners };
  const { conditions, actions } = lowerRoot(ctx, trigger);
  if (ctx.issues.length > 0) return { result: { ok: false, issues: ctx.issues }, owners };
  return { result: { ok: true, value: assembled(flow, written, conditions, actions) }, owners };
}

export function flowToDefinition(flow: Flow): FlowResult<Definition> {
  return lowerFlow(flow).result;
}

type StepPaths = { byPath: Map<string, string>; lastDelay: string | null };

function walkSteps(owners: Map<object, string>, list: Action[], base: string, into: StepPaths): void {
  list.forEach((action, i) => {
    const path = `${base}[${i}]`;
    const id = owners.get(action);
    if (id) into.byPath.set(path, id);
    if (id && action.type === 'delay') into.lastDelay = id;
    const gate = action.type === 'if' ? owners.get(action.params?.condition as object) : undefined;
    if (gate) into.byPath.set(`${path}.params.condition`, gate);
    for (const arm of armsOf(action)) walkSteps(owners, arm.list, `${path}.params.${arm.key}`, into);
  });
}

export type FlowStepRef = { index: number; type: string; node: string; arm: string };

type StepRefPlace = { top: number | null; path: string };

function collectStepRefs(owners: Map<object, string>, list: Action[], place: StepRefPlace, into: FlowStepRef[]): void {
  list.forEach((action, i) => {
    const index = place.top ?? i;
    const node = owners.get(action);
    if (node) into.push({ index, type: action.type, node, arm: place.path });
    for (const arm of armsOf(action)) collectStepRefs(owners, arm.list, { top: index, path: `${place.path}/${i}.${arm.key}` }, into);
  });
}

// A run record only carries its top-level action index and type, so this is what links one back to a drawn step.
export function flowStepRefs(flow: Flow): FlowStepRef[] {
  const { result, owners } = lowerFlow(flow);
  if (!result.ok) return [];
  const into: FlowStepRef[] = [];
  collectStepRefs(owners, result.value.actions ?? [], { top: null, path: '' }, into);
  return into;
}

function stepPaths(definition: Definition, owners: Map<object, string>): StepPaths {
  const into: StepPaths = { byPath: new Map(), lastDelay: null };
  (definition.conditions ?? []).forEach((condition, i) => {
    const id = owners.get(condition);
    if (id) into.byPath.set(`conditions[${i}]`, id);
  });
  walkSteps(owners, definition.actions ?? [], 'actions', into);
  return into;
}

const within = (path: string, key: string) => path === key || path.startsWith(`${key}.`);

function deepestStep(path: string, found: StepPaths): string {
  return [...found.byPath.keys()].filter((key) => within(path, key)).reduce((best, key) => (key.length > best.length ? key : best), '');
}

function locateIssue(issue: Issue, found: StepPaths): Issue {
  const budget = issue.path === 'actions' && issue.code === CODE.delayBudget;
  if (budget && found.lastDelay) return { path: nodePath(found.lastDelay), code: issue.code };
  const key = deepestStep(issue.path, found);
  if (!key) return issue;
  return { path: `${nodePath(found.byPath.get(key)!)}${issue.path.slice(key.length)}`, code: issue.code };
}

export function locateIssues(flow: Flow, issues: Issue[]): Issue[] {
  const { result, owners } = lowerFlow(flow);
  if (!result.ok) return issues;
  const found = stepPaths(result.value, owners);
  return issues.map((issue) => locateIssue(issue, found));
}

type Tail = { from: string; port: string };

type Building = { nodes: FlowNode[]; edges: FlowEdge[]; issues: Issue[] };

function addNode(b: Building, type: string, config: FlowConfig, tails: Tail[]): string {
  const id = `n${b.nodes.length + 1}`;
  b.nodes.push({ id, type, x: 0, y: 0, config });
  for (const tail of tails) b.edges.push({ id: `e${b.edges.length + 1}`, from: tail.from, port: tail.port, to: id });
  return id;
}

function eventNode(params: Params): { type: string; config: FlowConfig } | null {
  const kind = str(params.kind);
  const filters = (params.filters ?? {}) as Record<string, string>;
  const filterKeys = Object.keys(filters);
  const fixed = Object.keys(FIXED_EVENT_NODES).find((type) => FIXED_EVENT_NODES[type] === kind);
  if (fixed) return filterKeys.length === 0 ? { type: fixed, config: {} } : null;
  const threshold = THRESHOLD_FILTERS[kind];
  if (threshold) return thresholdNode(kind, threshold, filters);
  return subNode(kind, filters);
}

function thresholdNode(kind: string, threshold: { key: string; filter: string }, filters: Record<string, string>) {
  const keys = Object.keys(filters);
  if (keys.some((key) => key !== threshold.filter)) return null;
  const type = kind === 'channel.cheer' ? 'twitch.cheer' : 'twitch.raid';
  const min = filters[threshold.filter];
  return { type, config: min ? { [threshold.key]: Number(min) } : {} };
}

function onlyNotGift(filters: Record<string, string>): boolean {
  return Object.keys(filters).length === 1 && filters.is_gift === 'false';
}

function subNode(kind: string, filters: Record<string, string>): { type: string; config: FlowConfig } | null {
  if (kind === 'channel.subscribe' && onlyNotGift(filters)) {
    return { type: 'twitch.sub', config: { variant: 'new' } };
  }
  if (Object.keys(filters).length > 0) return null;
  const variant = Object.keys(SUB_KINDS).find((v) => v !== 'new' && SUB_KINDS[v] === kind);
  return variant ? { type: 'twitch.sub', config: { variant } } : null;
}

const PLAIN_COMMAND_KEYS = new Set(['name', 'aliases', 'match', 'sources']);

function wordNode(params: Params): { type: string; config: FlowConfig } | null {
  const pattern = str(params.name);
  if (!pattern.startsWith(WORD_START) || !pattern.endsWith(WORD_END)) return null;
  const word = unescapeWord(pattern.slice(WORD_START.length, pattern.length - WORD_END.length));
  return wordPattern(word) === pattern ? { type: 'twitch.chat.word', config: { word } } : null;
}

function isPlainChatCommand(params: Params): boolean {
  const sources = (params.sources ?? ['chat']) as string[];
  const plain = Object.keys(params).every((key) => PLAIN_COMMAND_KEYS.has(key));
  return plain && sources.length === 1 && sources[0] === 'chat';
}

function commandNode(params: Params): { type: string; config: FlowConfig } | null {
  if (!isPlainChatCommand(params)) return null;
  if (params.match === 'regex') return wordNode(params);
  if ((params.match ?? 'start') !== 'start') return null;
  const aliases = (params.aliases ?? []) as string[];
  const config: FlowConfig = { trigger: str(params.name) };
  if (aliases.length > 0) config.aliases = aliases.join(',');
  return { type: 'twitch.chat.command', config };
}

function triggerToNode(trigger: Trigger): { type: string; config: FlowConfig } | null {
  const params = trigger.params ?? {};
  if (trigger.type === 'command') return commandNode(params);
  if (trigger.type === 'event') return eventNode(params);
  if (trigger.type === 'reward') return { type: 'twitch.reward', config: { reward: str(params.reward_id) } };
  return null;
}

function cooldownNode(params: Params): { type: string; config: FlowConfig } | null {
  const global = num(params.global_s);
  const user = num(params.user_s);
  if (global > 0 && user > 0) return null;
  return { type: 'cond.cooldown', config: { seconds: global || user, perUser: user > 0 } };
}

function userNode(params: Params): { type: string; config: FlowConfig } | null {
  const ids = (params.allow_ids ?? []) as string[];
  const others = [params.allow, params.deny, params.deny_ids].flatMap((list) => (list ?? []) as string[]);
  const everyone = [...ids, ...others];
  if (everyone.length === 1 && (params.allow as string[] | undefined)?.[0] === SETUP_LOGIN) return { type: 'cond.user', config: { login: SETUP_LOGIN } };
  return ids.length === 1 && others.length === 0 ? { type: 'cond.user', config: { userId: ids[0]! } } : null;
}

function compareNode(params: Params): { type: string; config: FlowConfig } | null {
  if (!COMPARE_OPS.includes(str(params.op))) return null;
  return { type: 'cond.compare', config: { left: str(params.left), op: str(params.op), right: str(params.right) } };
}

const varConfig = (params: Params): FlowConfig => ({ scope: str(params.scope), name: str(params.name) });

function varConditionNode(params: Params): { type: string; config: FlowConfig } {
  const op = str(params.op);
  const config: FlowConfig = { ...varConfig(params), op };
  return { type: 'cond.var', config: op === 'exists' ? config : { ...config, value: str(params.value) } };
}

function conditionToNode(condition: Condition): { type: string; config: FlowConfig } | null {
  const params = condition.params ?? {};
  if (condition.type === 'var') return varConditionNode(params);
  if (condition.type === 'compare') return compareNode(params);
  if (condition.type === 'users') return userNode(params);
  if (condition.type === 'perm') return { type: 'cond.role', config: { level: str(params.level) } };
  if (condition.type === 'chance') return { type: 'cond.chance', config: { percent: num(params.percent) } };
  if (condition.type === 'cooldown') return cooldownNode(params);
  return null;
}

function lightsNode(params: Params): { type: string; config: FlowConfig } | null {
  const config: FlowConfig = { device: str(params.device), sku: str(params.sku) };
  if (str(params.device_name)) config.deviceName = str(params.device_name);
  const mode = str(params.mode) || 'color';
  if (mode === 'color') return { type: 'reply.lights', config: { ...config, mode, color: str(params.color) } };
  if (mode !== 'scene') return null;
  const scene: FlowConfig = {
    scene: str(params.scene_id),
    sceneType: str(params.scene_type),
    sceneInstance: str(params.scene_instance),
    sceneValue: str(params.scene_value)
  };
  if (str(params.scene_name)) scene.sceneName = str(params.scene_name);
  return { type: 'reply.lights', config: { ...config, mode, ...scene } };
}

function textNode(params: Params): { type: string; config: FlowConfig } {
  const op = str(params.op);
  const config: FlowConfig = { op, input: str(params.input), saveAs: str(params.save_as) };
  const [first, second] = TEXT_ARG_FIELDS.get(op) ?? [];
  if (first) config[first] = str(params.arg1);
  if (second) config[second] = str(params.arg2);
  return { type: 'reply.text', config };
}

function rewardEnabledChoice(enabled: unknown): string {
  if (enabled === true) return 'on';
  return enabled === false ? 'off' : 'keep';
}

function rewardUpdateNode(params: Params): { type: string; config: FlowConfig } {
  const config: FlowConfig = { reward: str(params.reward_id), setEnabled: rewardEnabledChoice(params.enabled), reset: params.reset_on_stream_end === true };
  return { type: 'reply.reward_update', config: typeof params.cost === 'number' ? { ...config, cost: params.cost } : config };
}

const API_READERS: Record<string, (params: Params) => { type: string; config: FlowConfig }> = {
  http_get: (params) => ({
    type: 'reply.http_get',
    config: { url: str(params.url), jsonPath: ((params.json_path ?? []) as string[]).join(JSON_PATH_SEPARATOR), saveAs: str(params.save_as) }
  }),
  http_post: (params) => ({ type: 'reply.http_post', config: { url: str(params.url), body: str(params.body), saveAs: str(params.save_as) } })
};

const ACTION_READERS: Record<string, (params: Params) => { type: string; config: FlowConfig } | null> = {
  chat: (params) => ({ type: 'reply.chat', config: { message: str(params.text) } }),
  lights: lightsNode,
  points_add: (params) => (params.login ? null : { type: 'reply.points', config: { amount: num(params.amount) } }),
  counter_add: (params) => (num(params.amount) === 1 ? { type: 'reply.counter', config: { counter: str(params.name) } } : null),
  delay: (params) => ({ type: 'reply.wait', config: { seconds: num(params.ms) / 1000 } }),
  var_set: (params) => ({ type: 'reply.var_set', config: { ...varConfig(params), value: str(params.value) } }),
  var_add: (params) => ({ type: 'reply.var_add', config: { ...varConfig(params), amount: num(params.amount) } }),
  var_delete: (params) => ({ type: 'reply.var_delete', config: varConfig(params) }),
  text: textNode,
  math: (params) => ({ type: 'reply.math', config: { expression: str(params.expression), saveAs: str(params.save_as) } }),
  reward_update: rewardUpdateNode
};

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
  const worked = placeActions(b, (params.on_ok ?? []) as Action[], `${path}.params.on_ok`, [{ from: id, port: 'ok' }]);
  const failed = placeActions(b, (params.on_failure ?? []) as Action[], `${path}.params.on_failure`, [{ from: id, port: 'failure' }]);
  return [...worked, ...failed];
}

function placeAction(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  if (action.type === 'if') return placeIf(b, action, path, tails);
  if (action.type === 'parallel') return placeParallel(b, action, path, tails);
  if (action.type === 'loop') return placeLoop(b, action, path, tails);
  if (Object.hasOwn(API_READERS, action.type)) return placeApi(b, action, path, tails);
  const node = Object.hasOwn(ACTION_READERS, action.type) ? ACTION_READERS[action.type]!(action.params ?? {}) : null;
  if (!node) {
    b.issues.push({ path, code: FLOW_ISSUE.unsupportedAction });
    return tails;
  }
  return [{ from: addNode(b, node.type, placedConfig(action, node.config), tails), port: 'next' }];
}

function placeActions(b: Building, actions: Action[], path: string, tails: Tail[]): Tail[] {
  return actions.reduce<Tail[]>((open, action, i) => placeAction(b, action, `${path}[${i}]`, open), tails);
}

function placeConditions(b: Building, conditions: Condition[], tails: Tail[]): Tail[] {
  return conditions.reduce<Tail[]>((open, condition, i) => {
    const gate = placeGate(b, condition, `conditions[${i}]`, open);
    return gate ? [{ from: gate, port: 'yes' }] : open;
  }, tails);
}

type CounterScan = { nodes: FlowNode[]; kind: string; outs: Map<string, FlowEdge[]>; memo: Map<string, Set<string>> };

function raiseTemplate(scan: CounterScan, node: FlowNode, field: string): string {
  const counters = priorCounters(scan.nodes, scan.outs, scan.memo, node.id);
  const text = eventTokensToBuilder(scan.kind, str(node.config[field]), counters.length > 0);
  if (counters.length === 1 && !EVENT_TOKENS[scan.kind]?.count) return text.replaceAll(`{counter:${counters[0]}}`, '{count}');
  return text;
}

function raiseMessages(b: Building, kind: string): void {
  const flow = { nodes: b.nodes, edges: b.edges } as Flow;
  const scan: CounterScan = { nodes: b.nodes, kind, outs: outgoing(flow), memo: new Map() };
  for (const node of b.nodes) {
    for (const field of TEMPLATE_FIELDS[node.type] ?? []) {
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
