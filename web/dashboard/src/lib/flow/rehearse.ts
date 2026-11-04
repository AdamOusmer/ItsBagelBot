// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { FlowIconName as IconName } from './icons';
import { COMMAND_SAMPLES, rehearseAutomation, type ModuleFlags, type RehearsedLine, type Samples, type Seg } from '@bagel/kit/engine/rehearsal';
import { describe, optionLabel, portLabel } from './describe';
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

export type StepContext = {
  node: FlowNode;
  spec: NodeSpec;
  samples: Record<string, string>;
  segments: (template: string) => Seg[];
  value: (template: string) => string;
  lines: (template: string) => RehearsedLine[];
};

type Rehearser = (ctx: StepContext) => StepBody;

const sampleOf = (ctx: StepContext, key: string) => ctx.samples[key] ?? COMMAND_SAMPLES[key] ?? '';

const event = (template: string): Rehearser => (ctx) => ({ kind: 'event', segments: ctx.segments(template) });

const atLeast = (key: string, config: string, template: string): Rehearser => (ctx) => {
  const floor = Number(ctx.node.config[config]);
  if (floor > Number(ctx.samples[key])) ctx.samples[key] = String(floor);
  return event(template)(ctx);
};

const SUB_EVENTS: Record<string, string> = {
  resub: '{user} resubscribed for {months} months',
  gift: '{user} gifted a subscription'
};

function comparePasses(ctx: StepContext): boolean {
  const left = ctx.value(String(ctx.node.config.left ?? ''));
  const right = ctx.value(String(ctx.node.config.right ?? ''));
  switch (ctx.node.config.op) {
    case 'lt':
      return Number(left) < Number(right);
    case 'eq':
      return left === right;
    case 'contains':
      return left.includes(right);
    default:
      return Number(left) >= Number(right);
  }
}

const REHEARSERS: Readonly<Record<string, Rehearser>> = {
  'twitch.chat.command': (ctx) => ({
    kind: 'viewer',
    name: sampleOf(ctx, 'user'),
    text: `${String(ctx.node.config.trigger ?? '') || '!command'} ${sampleOf(ctx, 'args')}`
  }),
  'twitch.chat.word': (ctx) => ({
    kind: 'viewer',
    name: sampleOf(ctx, 'user'),
    text: String(ctx.node.config.word ?? '') || 'bagel'
  }),
  'twitch.follow': event('{user} followed the channel'),
  'twitch.sub': (ctx) => event(SUB_EVENTS[String(ctx.node.config.variant)] ?? '{user} subscribed at tier {tier}')(ctx),
  'twitch.cheer': atLeast('bits', 'minBits', '{user} cheered {bits} bits'),
  'twitch.raid': atLeast('viewers', 'minViewers', '{user} raided with {viewers} viewers'),
  'twitch.reward': (ctx) => event(`{user} redeemed ${optionLabel(ctx.spec, ctx.node, 'reward') || 'a reward'}`)(ctx),
  'twitch.stream.online': event('The stream went live: {title}'),
  'cond.compare': (ctx) => ({ kind: 'check', segments: ctx.segments(describe(ctx.node)), passes: comparePasses(ctx) }),
  'reply.chat': (ctx) => ({ kind: 'bot', lines: ctx.lines(String(ctx.node.config.message ?? '')) })
};

const BY_KIND: Readonly<Record<NodeSpec['kind'], Rehearser>> = {
  trigger: (ctx) => ({ kind: 'event', segments: ctx.segments(describe(ctx.node)) }),
  condition: (ctx) => ({ kind: 'check', segments: ctx.segments(describe(ctx.node)), passes: true }),
  reply: (ctx) => ({ kind: 'effect', segments: ctx.segments(describe(ctx.node)) })
};

function contextFor(node: FlowNode, spec: NodeSpec, samples: Record<string, string>, modules: ModuleFlags): StepContext {
  const lines = (template: string) => rehearseAutomation(template, samples as Samples, modules);
  const segments = (template: string) => lines(template)[0]?.segments ?? [];
  const value = (template: string) => segments(template).map((seg) => seg.text).join('');
  return { node, spec, samples, segments, value, lines };
}

function provide(spec: NodeSpec, samples: Record<string, string>) {
  for (const v of spec.provides ?? []) samples[v.token.slice(1, -1)] = v.sample;
}

function toStep(ctx: StepContext, chosen: string | undefined, via: string | undefined): RehearsalStep {
  const body = (REHEARSERS[ctx.node.type] ?? BY_KIND[ctx.spec.kind])(ctx);
  const base = { id: ctx.node.id, icon: ctx.spec.icon, label: ctx.spec.label, via };
  if (body.kind !== 'check') return { ...base, ...body };
  const choices = outputsFor(ctx.spec).map((o) => ({ id: o.id, label: portLabel(ctx.node, o) }));
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

function nextPort(step: RehearsalStep, spec: NodeSpec): string {
  return step.kind === 'check' ? step.taken : outputsFor(spec)[0].id;
}

export type RehearsalOptions = {
  decisions?: Decisions;
  until?: string;
  modules?: ModuleFlags;
};

export function rehearseFlow(flow: Flow, { decisions = {}, until, modules = {} }: RehearsalOptions = {}): RehearsalStep[] {
  const traced = until ? traceBack(flow, until) : { root: firstTrigger(flow), forced: {} as Record<string, string> };
  const samples: Record<string, string> = {};
  const steps: RehearsalStep[] = [];
  const seen = new Set<string>();
  let done = false;

  const visit = (id: string, via?: string) => {
    const node = flow.nodes.find((n) => n.id === id);
    const spec = node && specFor(node.type);
    if (done || !node || !spec || seen.has(id)) return;
    seen.add(id);
    provide(spec, samples);
    const step = toStep(contextFor(node, spec, samples, modules), traced.forced[id] ?? decisions[id], via);
    steps.push(step);
    done = id === until;
    const port = nextPort(step, spec);
    for (const e of flow.edges) if (e.from === id && e.port === port) visit(e.to, e.id);
  };

  if (traced.root) visit(traced.root);
  return steps;
}
