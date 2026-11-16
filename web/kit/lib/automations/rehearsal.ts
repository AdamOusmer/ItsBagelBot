// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { responseLines } from '../engine/commands-validate';
import { MAX_ACTIONS, MAX_DELAY_MS } from './limits';
import { applyAction, createState, evaluateCondition, expandTemplate } from './rehearsal-state';
import { matchTrigger, scenarioArgs, scenarioEvent } from './rehearsal-scenario';
import type { Action, Condition, Definition } from './types';
import type { RehearsalContext, RehearsalEvent, RehearsalOverrides, RehearsalResult, RehearsalScenario } from './rehearsal-types';

export type * from './rehearsal-types';
export { SCENARIO_FIELDS, defaultScenario, matchTrigger, scenarioKind } from './rehearsal-scenario';

type Path = string;
type Frame = { context: RehearsalContext; lane: number[]; iteration: number[]; delayMs: number };
type EventBody = Omit<RehearsalEvent, 'key' | 'path' | 'atMs' | 'lane' | 'iteration'>;
type Pending = { frame: Frame; work: Generator<number, void> };
type Execution = { events: RehearsalEvent[]; pending: Pending[]; effects: number; visits: number };
type Step = { action: Action; path: Path };

function emit(run: Execution, frame: Frame, body: EventBody): void {
  const { path, atMs } = frame.context;
  const lane = [...frame.lane];
  const iteration = [...frame.iteration];
  run.events.push({ ...body, key: `${path}:${lane.join('.')}:${iteration.join('.')}:${run.events.length}`, path, lane, iteration, atMs });
}

function contextFor(definition: Definition, scenario: RehearsalScenario, overrides: RehearsalOverrides): RehearsalContext {
  const args = scenarioArgs(definition, scenario);
  const kind = definition.trigger.type === 'reward' ? 'channel.channel_points_custom_reward_redemption.add' : String(definition.trigger.params?.kind ?? '');
  const event = scenarioEvent(scenario, kind);
  const values: Record<string, string> = {
    args, message: scenario.message ?? '', command: String(definition.trigger.params?.name ?? ''),
    ...Object.fromEntries(Object.entries(event).map(([key, value]) => [`event.${key}`, value])),
    ...scenario.values
  };
  addViewerValues(values, scenario, args);
  return { scenario: { ...scenario, args, values }, state: createState(scenario), overrides, locals: {}, cooldownClaims: {}, path: 'trigger', atMs: 0 };
}

function addViewerValues(values: Record<string, string>, scenario: RehearsalScenario, args: Path): void {
  if (scenario.user !== undefined) values.user = values.sender = scenario.user;
  if (scenario.userId !== undefined) values['user.id'] = values.userid = scenario.userId;
  if (scenario.user !== undefined) values['user.login'] = scenario.user.toLowerCase();
  const target = args.split(/\s+/)[0]?.replace(/^@/, '');
  if (target) values.touser = values.target = target;
}

function check(condition: Condition, run: Execution, frame: Frame): boolean {
  const passes = evaluateCondition(condition, frame.context);
  emit(run, frame, { kind: 'check', status: passes ? 'yes' : 'no', port: passes ? 'yes' : 'no', detail: { type: condition.type } });
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

function limited(step: Step, run: Execution, frame: Frame): boolean {
  frame.context.path = step.path;
  run.visits++;
  if (run.visits <= 4 * MAX_ACTIONS) return false;
  emit(run, frame, { kind: 'skip', status: 'failed', detail: { code: 'max_steps' } });
  return true;
}

function* list(actions: Action[], base: Path, run: Execution, frame: Frame): Generator<number, void> {
  for (const [index, action] of actions.entries()) {
    const step = { action, path: `${base}[${index}]` };
    if (limited(step, run, frame)) return;
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

function loopTimes(value: Path): number | null {
  const trimmed = value.trim();
  if (!/^[+-]?\d+$/.test(trimmed)) return null;
  return Math.max(0, Number(trimmed));
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
  const limit = yield* iterations({ step, times }, run, frame);
  frame.iteration = outer;
  if (prior === undefined) delete frame.context.locals.index;
  else frame.context.locals.index = prior;
  frame.context.path = step.path;
  emit(run, frame, { kind: 'loop', status: limit ? 'failed' : 'ok', port: 'done', detail: limit ? { code: limit } : undefined });
}

function* iterations(cursor: { step: Step; times: number }, run: Execution, frame: Frame): Generator<number, Path | undefined> {
  const outer = frame.iteration;
  const body = (cursor.step.action.params?.actions ?? []) as Action[];
  if (body.length === 0) return;
  for (let index = 1; index <= cursor.times; index++) {
    if (run.effects >= MAX_ACTIONS) return 'max_actions';
    if (run.visits >= 4 * MAX_ACTIONS) return 'max_steps';
    frame.iteration = [...outer, index];
    frame.context.locals.index = String(index);
    yield* list(body, `${cursor.step.path}.params.actions`, run, frame);
  }
}

function* http(step: Step, run: Execution, frame: Frame): Generator<number, void> {
  const outcome = applyAction(step.action, frame.context);
  const success = outcome.status === 'ok';
  const port = success ? 'ok' : 'failure';
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

function effect(step: Step, run: Execution, frame: Frame): void {
  const params = step.action.params ?? {};
  const detail = Object.fromEntries(Object.entries(params).map(([key, value]) => [key, expandTemplate(String(value), frame.context).text]));
  const outcome = applyAction(step.action, frame.context);
  emit(run, frame, { kind: 'effect', ...outcome, detail: { type: step.action.type, ...detail, ...outcome.detail } });
}

function say(step: Step, run: Execution, frame: Frame): void {
  const expanded = expandTemplate(String(step.action.params?.text ?? ''), frame.context);
  const lines = responseLines(expanded.text).filter((line) => line.trim() !== '');
  emit(run, frame, { kind: 'say', status: lines.length > 0 ? 'ok' : 'skipped', lines, detail: { missing: expanded.missing.join(',') } });
}

type Handler = (step: Step, run: Execution, frame: Frame) => Generator<number, void>;
const CONTROL: Readonly<Record<string, Handler>> = { if: branch, loop };
const EFFECTS: Readonly<Record<string, Handler>> = { delay: wait, http_get: http, http_post: http };

function* execute(step: Step, run: Execution, frame: Frame): Generator<number, void> {
  if (step.action.enabled === false) {
    emit(run, frame, { kind: 'skip', status: 'skipped', detail: { code: 'disabled' } });
    return;
  }
  if (step.action.type === 'parallel') return parallel(step, run, frame);
  const control = CONTROL[step.action.type];
  if (control) return yield* control(step, run, frame);
  if (++run.effects > MAX_ACTIONS) {
    emit(run, frame, { kind: 'skip', status: 'failed', detail: { code: 'max_actions' } });
    return;
  }
  const handler = EFFECTS[step.action.type];
  if (handler) return yield* handler(step, run, frame);
  if (step.action.type === 'chat') return say(step, run, frame);
  effect(step, run, frame);
}

function gates(definition: Definition, run: Execution, frame: Frame): boolean {
  return (definition.conditions ?? []).every((condition, index) => {
    frame.context.path = `conditions[${index}]`;
    return check(condition, run, frame);
  });
}

export function rehearseDefinition(definition: Definition, scenario: RehearsalScenario, overrides: RehearsalOverrides = {}): RehearsalResult {
  const matched = matchTrigger(definition, scenario);
  if (!matched.starts) return { starts: false, reason: { reason: matched.reason, values: matched.values }, events: [] };
  const context = contextFor(definition, scenario, overrides);
  const frame: Frame = { context, lane: [], iteration: [], delayMs: 0 };
  const run: Execution = { events: [], pending: [], effects: 0, visits: 0 };
  emit(run, frame, { kind: 'trigger', status: 'ok' });
  if (!gates(definition, run, frame)) return { starts: false, reason: { reason: 'condition_failed', values: { path: context.path } }, events: run.events };
  runUntilPause(run, { frame, work: list(definition.actions ?? [], 'actions', run, frame) });
  drain(run);
  run.events.sort((a, b) => a.atMs - b.atMs);
  return { starts: true, events: run.events };
}
