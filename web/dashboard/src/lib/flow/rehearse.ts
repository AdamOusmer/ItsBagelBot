// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { FlowIconName as IconName } from './icons';
import { comparePasses as compare } from '@bagel/kit/automations/compare';
import { COMMAND_SAMPLES, rehearseAutomation, type ModuleFlags, type RehearsedLine, type Samples, type Seg } from '@bagel/kit/engine/rehearsal';
import { stepLabel } from './copy';
import { describe, optionLabel, portLabel } from './describe';
import type { Translate } from './messages';
import { outputsFor, specFor, type Flow, type FlowNode, type NodeSpec } from './model';

export type Decisions = Readonly<Record<string, string>>;

export type BranchChoice = { id: string; label: string };

type StepBody =
  | { kind: 'viewer'; name: string; text: string }
  | { kind: 'event'; segments: Seg[] }
  | { kind: 'check'; segments: Seg[]; passes: boolean }
  | { kind: 'bot'; lines: RehearsedLine[] }
  | { kind: 'effect'; segments: Seg[] };

export type RehearsalStep = { id: string; icon: IconName; label: string; via?: string } & (
  | Exclude<StepBody, { kind: 'check' }>
  | { kind: 'check'; segments: Seg[]; taken: string; choices: BranchChoice[] }
);

export type StepWords = {
  label: string;
  line: string;
  event: string;
  fallback: string;
  choices: BranchChoice[];
};

export type StepContext = {
  node: FlowNode;
  spec: NodeSpec;
  words: StepWords;
  samples: Record<string, string>;
  segments: (template: string) => Seg[];
  value: (template: string) => string;
  lines: (template: string) => RehearsedLine[];
};

type Rehearser = (ctx: StepContext) => StepBody;

const sampleOf = (ctx: StepContext, key: string) => ctx.samples[key] ?? COMMAND_SAMPLES[key] ?? '';

const event: Rehearser = (ctx) => ({ kind: 'event', segments: ctx.segments(ctx.words.event) });

const atLeast = (key: string, config: string): Rehearser => (ctx) => {
  const floor = Number(ctx.node.config[config]);
  if (floor > Number(ctx.samples[key])) ctx.samples[key] = String(floor);
  return event(ctx);
};

function subEvent(t: Translate, node: FlowNode): string {
  const variant = String(node.config.variant);
  if (variant === 'resub') return t('flows.line.eventResub');
  return variant === 'gift' ? t('flows.line.eventGift') : t('flows.line.eventSub');
}

function rewardEvent(t: Translate, spec: NodeSpec, node: FlowNode): string {
  const reward = optionLabel(t, spec, node, 'reward');
  return reward ? t('flows.line.eventRewardNamed', { reward }) : t('flows.line.eventReward');
}

function eventLine(t: Translate, spec: NodeSpec, node: FlowNode): string {
  switch (node.type) {
    case 'twitch.follow':
      return t('flows.line.eventFollow');
    case 'twitch.sub':
      return subEvent(t, node);
    case 'twitch.cheer':
      return t('flows.line.eventCheer');
    case 'twitch.raid':
      return t('flows.line.eventRaid');
    case 'twitch.reward':
      return rewardEvent(t, spec, node);
    case 'twitch.stream.online':
      return t('flows.line.eventLive');
    default:
      return '';
  }
}

function fallbackText(t: Translate, node: FlowNode): string {
  if (node.type === 'twitch.chat.command') return t('flows.line.sampleCommand');
  return node.type === 'twitch.chat.word' ? t('flows.line.sampleWord') : '';
}

function wordsFor(t: Translate, spec: NodeSpec, node: FlowNode): StepWords {
  return {
    label: stepLabel(t, spec),
    line: describe(t, node),
    event: eventLine(t, spec, node),
    fallback: fallbackText(t, node),
    choices: outputsFor(spec).map((o) => ({ id: o.id, label: portLabel(t, node, o.id) }))
  };
}

function comparePasses(ctx: StepContext): boolean {
  const left = ctx.value(String(ctx.node.config.left ?? ''));
  const right = ctx.value(String(ctx.node.config.right ?? ''));
  return compare(String(ctx.node.config.op ?? 'gte'), left, right);
}

const REHEARSERS: Readonly<Record<string, Rehearser>> = {
  'twitch.chat.command': (ctx) => ({
    kind: 'viewer',
    name: sampleOf(ctx, 'user'),
    text: `${String(ctx.node.config.trigger ?? '') || ctx.words.fallback} ${sampleOf(ctx, 'args')}`
  }),
  'twitch.chat.word': (ctx) => ({
    kind: 'viewer',
    name: sampleOf(ctx, 'user'),
    text: String(ctx.node.config.word ?? '') || ctx.words.fallback
  }),
  'twitch.follow': event,
  'twitch.sub': event,
  'twitch.cheer': atLeast('bits', 'minBits'),
  'twitch.raid': atLeast('viewers', 'minViewers'),
  'twitch.reward': event,
  'twitch.stream.online': event,
  'cond.compare': (ctx) => ({ kind: 'check', segments: ctx.segments(ctx.words.line), passes: comparePasses(ctx) }),
  'reply.chat': (ctx) => ({ kind: 'bot', lines: ctx.lines(String(ctx.node.config.message ?? '')) }),
  'reply.http_get': (ctx) => ({ kind: 'check', segments: ctx.segments(ctx.words.line), passes: true }),
  'reply.http_post': (ctx) => ({ kind: 'check', segments: ctx.segments(ctx.words.line), passes: true })
};

const BY_KIND: Readonly<Record<NodeSpec['kind'], Rehearser>> = {
  trigger: (ctx) => ({ kind: 'event', segments: ctx.segments(ctx.words.line) }),
  condition: (ctx) => ({ kind: 'check', segments: ctx.segments(ctx.words.line), passes: true }),
  reply: (ctx) => ({ kind: 'effect', segments: ctx.segments(ctx.words.line) })
};

type StepIdentity = Pick<StepContext, 'node' | 'spec' | 'words'>;

function contextFor({ node, spec, words }: StepIdentity, samples: Record<string, string>, modules: ModuleFlags): StepContext {
  const lines = (template: string) => rehearseAutomation(template, samples as Samples, modules);
  const segments = (template: string) => lines(template)[0]?.segments ?? [];
  const value = (template: string) => segments(template).map((seg) => seg.text).join('');
  return { node, spec, words, samples, segments, value, lines };
}

function provide(spec: NodeSpec, samples: Record<string, string>) {
  const viaPorts = (spec.outputs ?? []).flatMap((output) => output.provides ?? []);
  for (const v of [...(spec.provides ?? []), ...viaPorts]) samples[v.token.slice(1, -1)] = v.sample;
}

function toStep(ctx: StepContext, chosen: string | undefined, via: string | undefined): RehearsalStep {
  const body = (REHEARSERS[ctx.node.type] ?? BY_KIND[ctx.spec.kind])(ctx);
  const base = { id: ctx.node.id, icon: ctx.spec.icon, label: ctx.words.label, via };
  if (body.kind !== 'check') return { ...base, ...body };
  const choices = ctx.words.choices;
  const fallback = body.passes ? choices[0]?.id : choices[1]?.id;
  return { ...base, kind: 'check', segments: body.segments, taken: chosen ?? fallback ?? '', choices };
}

function traceBack(flow: Flow, target: string): { root: string; forced: Record<string, string> } {
  const forced: Record<string, string> = {};
  const seen = new Set([target]);
  let root = target;
  for (;;) {
    const edge = flow.edges.find((e) => e.to === root && !seen.has(e.from));
    if (!edge) return { root, forced };
    forced[edge.from] = edge.port;
    seen.add(edge.from);
    root = edge.from;
  }
}

function firstTrigger(flow: Flow): string | undefined {
  return flow.nodes.find((n) => specFor(n.type)?.kind === 'trigger')?.id;
}

function nextPorts(step: RehearsalStep, spec: NodeSpec): string[] {
  return step.kind === 'check' ? [step.taken] : outputsFor(spec).map((output) => output.id);
}

export type RehearsalOptions = {
  decisions?: Decisions;
  until?: string;
  modules?: ModuleFlags;
};

type Walk = {
  flow: Flow;
  decisions: Decisions;
  forced: Record<string, string>;
  until?: string;
  modules: ModuleFlags;
  samples: Record<string, string>;
  steps: RehearsalStep[];
  seen: Set<string>;
  done: boolean;
};

function unvisited(walk: Walk, id: string): { node: FlowNode; spec: NodeSpec } | undefined {
  if (walk.done || walk.seen.has(id)) return undefined;
  const node = walk.flow.nodes.find((n) => n.id === id);
  const spec = node && specFor(node.type);
  return node && spec ? { node, spec } : undefined;
}

function visit(t: Translate, walk: Walk, id: string, via?: string): void {
  const found = unvisited(walk, id);
  if (!found) return;
  const { node, spec } = found;
  walk.seen.add(id);
  provide(spec, walk.samples);
  const context = contextFor({ node, spec, words: wordsFor(t, spec, node) }, walk.samples, walk.modules);
  const step = toStep(context, walk.forced[id] ?? walk.decisions[id], via);
  walk.steps.push(step);
  walk.done = id === walk.until;
  for (const port of nextPorts(step, spec)) {
    for (const e of walk.flow.edges) if (e.from === id && e.port === port) visit(t, walk, e.to, e.id);
  }
}

export function rehearseFlow(flow: Flow, t: Translate, options: RehearsalOptions = {}): RehearsalStep[] {
  const { decisions = {}, until, modules = {} } = options;
  const traced = until ? traceBack(flow, until) : { root: firstTrigger(flow), forced: {} as Record<string, string> };
  const walk: Walk = { flow, decisions, forced: traced.forced, until, modules, samples: {}, steps: [], seen: new Set(), done: false };
  if (traced.root) visit(t, walk, traced.root);
  return walk.steps;
}
