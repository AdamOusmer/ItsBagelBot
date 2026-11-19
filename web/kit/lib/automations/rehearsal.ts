// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { MAX_RESPONSE_LINES } from '../importer/validate';
import { apiPorts, eventTokensToBuilder } from './flow-graph';
import { MAX_ACTIONS, MAX_DELAY_MS, MAX_LOOP_TIMES } from './limits';
import { applyAction, createState, evaluateCondition, expandTemplate, messageTarget } from './rehearsal-state';
import { matchTrigger, scenarioArgs, scenarioEvent, scenarioIdentity } from './rehearsal-scenario';
import { trimSpace } from './text';
import type { Action, Condition, Definition, Params } from './types';
import type { RehearsalContext, RehearsalEvent, RehearsalLineStyle, RehearsalOverrides, RehearsalResult, RehearsalScenario } from './rehearsal-types';

export type * from './rehearsal-types';
export { SCENARIO_FIELDS, defaultScenario, matchTrigger, scenarioArgs, scenarioIdentity, scenarioKind, scenarioPatches, scenarioStateFields } from './rehearsal-scenario';
export { compactDuration } from './rehearsal-state';
export { chatModeValueField, configKey, streamTargetNodes } from './stream-flow';

type Path = string;
type Frame = { context: RehearsalContext; lane: number[]; iteration: number[]; delayMs: number; stopped: boolean };
type EventBody = Omit<RehearsalEvent, 'key' | 'path' | 'atMs' | 'lane' | 'iteration'>;
type Pending = { frame: Frame; work: Generator<number, void> };
type Execution = { events: RehearsalEvent[]; pending: Pending[]; effects: number; visits: number };
type Step = { action: Action; path: Path };
type Check = { condition: Condition; path: Path };

function rootChecks(definition: Definition): Check[] {
  return (definition.conditions ?? []).map((condition, index) => ({ condition, path: `conditions[${index}]` }))
    .sort((a, b) => Number(a.condition.type === 'cooldown') - Number(b.condition.type === 'cooldown'));
}

const BRANCH_FIELDS: Readonly<Record<Path, readonly Path[]>> = {
  if: ['then', 'else'], loop: ['actions'], http_get: ['on_ok', 'on_failure'], http_post: ['on_ok', 'on_failure']
};

function* branchChecks(actions: Action[], base: Path): Generator<Check> {
  for (const [index, action] of actions.entries()) {
    const path = `${base}[${index}]`;
    const condition = action.params?.condition as Condition | undefined;
    if (action.type === 'if' && condition) yield { condition, path };
    yield* nestedChecks({ action, path });
  }
}

function* nestedChecks({ action, path }: Step): Generator<Check> {
  const params = action.params ?? {};
  if (action.type === 'parallel' || action.type === 'random_pick') {
    yield* forkChecks(params, path);
    return;
  }
  const fields = Object.hasOwn(BRANCH_FIELDS, action.type) ? BRANCH_FIELDS[action.type]! : [];
  for (const field of fields) {
    yield* branchChecks((params[field] ?? []) as Action[], `${path}.params.${field}`);
  }
}

function* forkChecks(params: Params, path: Path): Generator<Check> {
  const branches = (params.branches ?? []) as { actions?: Action[] }[];
  for (const [index, branch] of branches.entries()) {
    yield* branchChecks(branch.actions ?? [], `${path}.params.branches[${index}].actions`);
  }
}

function assignCooldownOrdinals(definition: Definition): ReadonlyMap<Path, number> {
  const ordinals = new Map<Path, number>();
  const checks = [...rootChecks(definition), ...branchChecks(definition.actions ?? [], 'actions')];
  for (const { condition, path } of checks) {
    if (condition.type === 'cooldown' && !condition.params?.name) ordinals.set(path, ordinals.size);
  }
  return ordinals;
}

function emit(run: Execution, frame: Frame, body: EventBody): void {
  const { path, atMs } = frame.context;
  const lane = [...frame.lane];
  const iteration = [...frame.iteration];
  run.events.push({ ...body, key: `${path}:${lane.join('.')}:${iteration.join('.')}:${run.events.length}`, path, lane, iteration, atMs });
}

function contextFor(definition: Definition, scenario: RehearsalScenario, overrides: RehearsalOverrides, formatDuration: RehearsalContext['formatDuration']): RehearsalContext {
  scenario = scenarioIdentity(definition, scenario);
  const args = scenarioArgs(definition, scenario);
  const kinds: Record<string, string> = { reward: 'channel.channel_points_custom_reward_redemption.add', countdown: 'countdown.finished' };
  const kind = kinds[definition.trigger.type] ?? String(definition.trigger.params?.kind ?? '');
  const values: Record<string, string> = {
    args, command: String(definition.trigger.params?.name ?? ''),
    ...eventValues(scenario, kind),
    ...scenario.values
  };
  addViewerValues(values, scenario, args);
  return {
    triggerType: definition.trigger.type, scenario: { ...scenario, args, values }, hasChatMessage: definition.trigger.type === 'command',
    state: createState(scenario), cooldownOrdinals: assignCooldownOrdinals(definition), overrides, locals: {}, path: 'trigger', atMs: 0, formatDuration
  };
}

const STREAM_ONLINE_TOKENS: Readonly<Record<string, string>> = { title: 'title', category: 'game' };

function eventValues(scenario: RehearsalScenario, kind: Path): Record<string, string> {
  const event = scenarioEvent(scenario, kind);
  const entries = Object.entries(event).flatMap(([key, value]) => {
    const token = `event.${key}`;
    const bare = kind === 'stream.online' ? STREAM_ONLINE_TOKENS[key] ?? token : eventTokensToBuilder(kind, `{${token}}`).slice(1, -1);
    const pair = [token, value] as const;
    return bare === token ? [pair] : [pair, [bare, value] as const];
  });
  return Object.fromEntries(entries);
}

function addViewerValues(values: Record<string, string>, scenario: RehearsalScenario, args: Path): void {
  if (scenario.user !== undefined) values.user = values.sender = scenario.user;
  if (scenario.userId !== undefined) values['user.id'] = values.userid = scenario.userId;
  if (scenario.user !== undefined) values['user.login'] = scenario.user.toLowerCase();
  values.touser = values.target = messageTarget(args, scenario.user ?? '');
}

function check(condition: Condition, run: Execution, frame: Frame): boolean {
  const { passes, code } = evaluateCondition(condition, frame.context);
  const port = passes ? 'yes' : 'no';
  const detail: Record<Path, Path> = { type: condition.type };
  if (code) detail.code = code;
  emit(run, frame, { kind: 'check', status: port, port, detail });
  return passes;
}

function runUntilPause(run: Execution, pending: Pending): void {
  const next = pending.work.next();
  if (next.done) return;
  pending.frame.context.atMs += next.value;
  run.pending.push(pending);
}

function drain(run: Execution): void {
  while (run.pending.length > 0) resumeNext(run);
}

function resumeNext(run: Execution): void {
  run.pending.sort((a, b) => a.frame.context.atMs - b.frame.context.atMs);
  runUntilPause(run, run.pending.shift()!);
}

function limited(budget: 'visits' | 'effects', run: Execution, frame: Frame): boolean {
  const maximum = budget === 'visits' ? 4 * MAX_ACTIONS : MAX_ACTIONS;
  if (++run[budget] <= maximum) return false;
  const code = budget === 'visits' ? 'max_steps' : 'max_actions';
  frame.stopped = true;
  emit(run, frame, { kind: 'skip', status: 'failed', detail: { code } });
  return true;
}

function* list(actions: Action[], base: Path, run: Execution, frame: Frame): Generator<number, void> {
  for (const [index, action] of actions.entries()) {
    const step = { action, path: `${base}[${index}]` };
    if (frame.stopped) return;
    frame.context.path = step.path;
    if (limited('visits', run, frame)) return;
    yield* execute(step, run, frame);
  }
}

function* branch(step: Step, run: Execution, frame: Frame): Generator<number, void> {
  const params = step.action.params ?? {};
  const passes = check(params.condition as Condition, run, frame);
  const key = passes ? 'then' : 'else';
  yield* list((params[key] ?? []) as Action[], `${step.path}.params.${key}`, run, frame);
}

function parallel(step: Step, run: Execution, frame: Frame): void {
  const branches = (step.action.params?.branches ?? []) as { actions?: Action[] }[];
  emit(run, frame, { kind: 'parallel', status: 'ok', detail: { branches: String(branches.length) } });
  branches.forEach((branch, index) => {
    const child: Frame = { ...frame, lane: [...frame.lane, index], iteration: [...frame.iteration], context: { ...frame.context, locals: { ...frame.context.locals } } };
    const work = list(branch.actions ?? [], `${step.path}.params.branches[${index}].actions`, run, child);
    runUntilPause(run, { frame: child, work });
  });
}

function* randomPick(step: Step, run: Execution, frame: Frame): Generator<number, void> {
  const branches = (step.action.params?.branches ?? []) as { actions?: Action[] }[];
  const requested = Number(frame.context.overrides[`${step.path}.path`]);
  const index = branches[requested - 1] ? requested - 1 : 0;
  emit(run, frame, { kind: 'effect', status: 'ok', port: `p${index + 1}`, detail: { path: String(index + 1), random: '1' } });
  yield* list(branches[index]?.actions ?? [], `${step.path}.params.branches[${index}].actions`, run, frame);
}

function* stop(_step: Step, run: Execution, frame: Frame): Generator<number, void> {
  frame.stopped = true;
  emit(run, frame, { kind: 'skip', status: 'skipped', detail: { code: 'stopped' } });
}

function loopTimes(value: Path): number | null {
  const trimmed = value.trim();
  if (!/^[+-]?\d+$/.test(trimmed)) return null;
  return Math.min(MAX_LOOP_TIMES, Math.max(0, Number(trimmed)));
}

function* loop(step: Step, run: Execution, frame: Frame): Generator<number, void> {
  const params = step.action.params ?? {};
  const times = loopTimes(expandTemplate(String(params.times ?? ''), frame.context).text);
  if (times === null) {
    emit(run, frame, { kind: 'loop', status: 'failed', detail: { code: 'bad_times' } });
    return;
  }
  emit(run, frame, { kind: 'loop', status: 'ok', port: 'body', detail: { times: String(times) } });
  const prior = frame.context.locals.index;
  const outer = frame.iteration;
  yield* iterations({ step, times }, run, frame);
  frame.iteration = outer;
  if (prior === undefined) delete frame.context.locals.index;
  else frame.context.locals.index = prior;
  frame.context.path = step.path;
  if (!frame.stopped) emit(run, frame, { kind: 'loop', status: 'ok', port: 'done' });
}

function* iterations(cursor: { step: Step; times: number }, run: Execution, frame: Frame): Generator<number, void> {
  const outer = frame.iteration;
  const body = (cursor.step.action.params?.actions ?? []) as Action[];
  if (body.length === 0) return;
  for (let index = 1; index <= cursor.times; index++) {
    if (frame.stopped) return;
    frame.iteration = [...outer, index];
    frame.context.locals.index = String(index);
    yield* list(body, `${cursor.step.path}.params.actions`, run, frame);
  }
}

function* branchingEffect(step: Step, run: Execution, frame: Frame): Generator<number, void> {
  const outcome = applyAction(step.action, frame.context);
  if (step.action.type === 'points_spend' && outcome.detail.code === 'no_viewer') {
    emit(run, frame, { kind: 'effect', ...outcome });
    return;
  }
  const success = outcome.status === 'ok';
  const [okPort, failurePort] = apiPorts(step.action.type);
  const port = success ? okPort : failurePort;
  const key = success ? 'on_ok' : 'on_failure';
  emit(run, frame, { kind: 'effect', ...outcome, port });
  yield* list((step.action.params?.[key] ?? []) as Action[], `${step.path}.params.${key}`, run, frame);
}

function* wait(step: Step, run: Execution, frame: Frame): Generator<number, void> {
  const ms = Number(step.action.params?.ms ?? 0);
  if (frame.delayMs + ms > MAX_DELAY_MS) {
    emit(run, frame, { kind: 'wait', status: 'failed', detail: { code: 'delay_budget' } });
    return;
  }
  frame.delayMs += ms;
  emit(run, frame, { kind: 'wait', status: 'ok', detail: { ms: String(ms) } });
  yield ms;
}

type Bubble = { text: string; style: RehearsalLineStyle };

const SLASH_COMMANDS = new Map<string, RehearsalLineStyle>([
  ['/announceblue', { mode: 'announce', color: 'blue' }],
  ['/announcegreen', { mode: 'announce', color: 'green' }],
  ['/announceorange', { mode: 'announce', color: 'orange' }],
  ['/announcepurple', { mode: 'announce', color: 'purple' }],
  ['/announce', { mode: 'announce', color: 'primary' }],
  ['/shoutout', { mode: 'shoutout' }],
  ['/pin', { mode: 'pin' }]
]);

function shoutoutBubble(rest: Path): Bubble {
  const [login = '', ...words] = rest.replace(/^ +/, '').split(' ');
  return { text: words.join(' ').replace(/^ +/, ''), style: { mode: 'shoutout', target: login.replace(/^@/, '') } };
}

function chatBubble(line: Path): Bubble {
  const space = line.indexOf(' ');
  const verb = space < 0 ? line : line.slice(0, space);
  const style = SLASH_COMMANDS.get(verb);
  if (!style) return { text: line, style: { mode: 'chat' } };
  const rest = space < 0 ? '' : line.slice(space + 1);
  return style.mode === 'shoutout' ? shoutoutBubble(rest) : { text: rest, style };
}

function messageBubbles(action: Action, message: Path): Bubble[] {
  if (action.type === 'announce') {
    return [{ text: message, style: { mode: 'announce', color: String(action.params?.color || 'primary') } }];
  }
  if (action.type === 'pin') return [{ text: message, style: { mode: 'pin' } }];
  return message.split('\n').filter((line) => trimSpace(line) !== '').slice(0, MAX_RESPONSE_LINES).map(chatBubble);
}

function hasBubbleContent({ style, text }: Bubble): boolean {
  return style.mode === 'shoutout' ? Boolean(style.target) : trimSpace(text) !== '';
}

function* lightsRestore(ms: number, run: Execution, frame: Frame, detail: EventBody['detail']): Generator<number, void> {
  yield ms;
  emit(run, frame, { kind: 'lights_restore', status: 'ok', detail });
}

function effect(step: Step, run: Execution, frame: Frame): void {
  const outcome = applyAction(step.action, frame.context);
  emit(run, frame, { kind: 'effect', ...outcome });
  if (step.action.type !== 'lights' || outcome.status !== 'ok') return;
  const holdMs = Number(step.action.params?.hold_s) * 1000;
  if (!(holdMs > 0)) return;
  const { count, device_name } = outcome.detail;
  const detail = { count: count!, ...(device_name === undefined ? {} : { device_name }) };
  const child: Frame = { ...frame, context: { ...frame.context } };
  runUntilPause(run, { frame: child, work: lightsRestore(holdMs, run, child, detail) });
}

function say(step: Step, run: Execution, frame: Frame): void {
  const { status, detail: outcome } = applyAction(step.action, frame.context);
  const bubbles = status === 'ok' ? messageBubbles(step.action, outcome.text ?? '').filter(hasBubbleContent) : [];
  const lines = bubbles.map((bubble) => bubble.text);
  const lineStyles = bubbles.map((bubble) => bubble.style);
  const detail = { ...lineStyles[0], missing: outcome.missing, ...(outcome.random ? { random: outcome.random } : {}), ...(!lines.length ? { code: outcome.code ?? 'empty' } : {}) };
  emit(run, frame, { kind: 'say', status: status === 'ok' && !lines.length ? 'skipped' : status, lines, lineStyles, detail });
}

type Handler = (step: Step, run: Execution, frame: Frame) => Generator<number, void>;
const CONTROL: Readonly<Record<string, Handler>> = { if: branch, loop, random_pick: randomPick };
const EFFECTS: Readonly<Record<string, Handler>> = { delay: wait, http_get: branchingEffect, http_post: branchingEffect, points_spend: branchingEffect, stop };

function* execute(step: Step, run: Execution, frame: Frame): Generator<number, void> {
  if (step.action.enabled === false) {
    emit(run, frame, { kind: 'skip', status: 'skipped', detail: { code: 'disabled' } });
    return;
  }
  if (step.action.type === 'parallel') return parallel(step, run, frame);
  const control = CONTROL[step.action.type];
  if (control) return yield* control(step, run, frame);
  if (limited('effects', run, frame)) return;
  const handler = EFFECTS[step.action.type];
  if (handler) return yield* handler(step, run, frame);
  if (['chat', 'announce', 'reply', 'pin'].includes(step.action.type)) return say(step, run, frame);
  effect(step, run, frame);
}

function gates(definition: Definition, run: Execution, frame: Frame): boolean {
  return rootChecks(definition).every(({ condition, path }) => {
    frame.context.path = path;
    return check(condition, run, frame);
  });
}

export function rehearseDefinition(definition: Definition, scenario: RehearsalScenario, overrides: RehearsalOverrides = {}, formatDuration?: RehearsalContext['formatDuration']): RehearsalResult {
  const matched = matchTrigger(definition, scenario);
  if (!matched.starts) return { starts: false, reason: { reason: matched.reason, values: matched.values }, events: [] };
  const context = contextFor(definition, scenario, overrides, formatDuration);
  const frame: Frame = { context, lane: [], iteration: [], delayMs: 0, stopped: false };
  const run: Execution = { events: [], pending: [], effects: 0, visits: 0 };
  emit(run, frame, { kind: 'trigger', status: 'ok' });
  if (!gates(definition, run, frame)) return { starts: false, reason: { reason: 'condition_failed', values: { path: context.path } }, events: run.events };
  runUntilPause(run, { frame, work: list(definition.actions ?? [], 'actions', run, frame) });
  drain(run);
  run.events.sort((a, b) => a.atMs - b.atMs);
  return { starts: true, events: run.events };
}
