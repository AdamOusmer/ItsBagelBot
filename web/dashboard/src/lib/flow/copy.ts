// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { MessageKey } from '@bagel/kit';
import { stepRequirement, type RequirementTrigger, type StepRequirement, type StepRole } from '@bagel/kit/automations';
import { MAX_WAIT_SECONDS, outputsFor, specFor, targetFieldFor, targetHasViewer, type FieldSpec, type Flow, type FlowNode, type NodeSpec, type StepGroup, type VariableSpec } from './model';
import type { FieldIssue } from './issues';
import { issueMessage, type Translate } from './messages';

export const stepLabel = (t: Translate, spec: NodeSpec): string => t(`flows.step.${spec.kind}.${spec.slug}.label` as MessageKey);

export const stepSummary = (t: Translate, spec: NodeSpec): string => t(`flows.step.${spec.kind}.${spec.slug}.summary` as MessageKey);

export function stepNote(t: Translate, spec: NodeSpec): string | undefined {
  const key = `flows.step.${spec.kind}.${spec.slug}.note` as MessageKey;
  return known(key, t(key)) || undefined;
}

const REQUIREMENT_TRIGGERS: Record<string, RequirementTrigger> = {
  'twitch.chat.command': { type: 'command' },
  'twitch.chat.word': { type: 'command' },
  'twitch.reward': { type: 'reward' },
  'twitch.unban_request': { type: 'event', kind: 'channel.unban_request.create' },
  'twitch.ad_break': { type: 'event', kind: 'channel.ad_break.begin' },
  'twitch.channel_update': { type: 'event', kind: 'channel.update' },
  'schedule.timer': { type: 'timer' },
  'schedule.manual': { type: 'manual' }
};

const ROLE_KNOWN_TRIGGERS = new Set(['twitch.chat.command', 'twitch.chat.word', 'countdown.finished', 'schedule.timer', 'schedule.manual']);

const TRIGGER_LABELS: Record<Extract<StepRequirement, { kind: 'trigger' }>['trigger'], MessageKey> = {
  command: 'flows.step.trigger.chatCommand.label',
  reward: 'flows.step.trigger.reward.label',
  unban_request: 'flows.step.trigger.unbanRequest.label'
};

const ROLE_LABELS: Record<StepRole, MessageKey> = {
  mod: 'perm.badge.mod',
  lead_mod: 'modules.permLeadMods',
  broadcaster: 'flows.test.role.broadcaster'
};

export function requirementTrigger(flow: Flow): RequirementTrigger {
  const trigger = flow.nodes.find((node) => specFor(node.type)?.kind === 'trigger');
  return REQUIREMENT_TRIGGERS[trigger?.type ?? ''] ?? { type: '' };
}

export function stepRequirementFor(spec: NodeSpec, trigger: RequirementTrigger, params?: Pick<FlowNode['config'], 'who'>): StepRequirement | null {
  if (spec.type === 'cond.pattern') return stepRequirement('arg_regex', trigger);
  if (spec.kind !== 'reply') return null;
  const action = spec.type === 'reply.stream_info' ? 'set_title' : spec.type.slice('reply.'.length);
  return stepRequirement(action, trigger, params);
}

function triggerRequirementText(t: Translate, requirement: Extract<StepRequirement, { kind: 'trigger' }>, tag: boolean, spec?: NodeSpec): string {
  const trigger = t(TRIGGER_LABELS[requirement.trigger]);
  if (tag) return t('flows.step.requirement.triggerTag', { trigger });
  const key = `flows.step.reply.${spec?.slug}.note` as MessageKey;
  const note = spec ? known(key, t(key)) : '';
  return note || t('flows.step.requirement.triggerNote', { trigger });
}

export function requirementText(t: Translate, requirement: StepRequirement, variant: 'tag' | 'note' | 'start' = 'tag', spec?: NodeSpec): string {
  if (requirement.kind === 'trigger') return triggerRequirementText(t, requirement, variant === 'tag', spec);
  const role = t(ROLE_LABELS[requirement.level]);
  if (variant === 'tag') return role;
  return t(variant === 'start' ? 'flows.step.requirement.startNote' : 'flows.step.requirement.roleNote', { role });
}

export function roleWarning(t: Translate, flow: Flow, node: FlowNode | undefined): string | undefined {
  if (node?.type !== 'cond.role') return undefined;
  const trigger = flow.nodes.find((entry) => specFor(entry.type)?.kind === 'trigger');
  const knowsRole = ROLE_KNOWN_TRIGGERS.has(trigger?.type ?? '');
  return knowsRole ? undefined : t('flows.step.condition.role.warning');
}

const viewerScoped = (node: FlowNode | undefined): boolean => node?.type === 'reply.var' && node.config.scope === 'viewer';

const viewerlessTrigger = (flow: Flow): boolean =>
  !targetHasViewer(flow.nodes.find((entry) => specFor(entry.type)?.kind === 'trigger')?.type);

export function scopeWarning(t: Translate, flow: Flow, node: FlowNode | undefined): string | undefined {
  return viewerScoped(node) && viewerlessTrigger(flow) ? t('flows.step.reply.varChange.scopeWarning') : undefined;
}

const neverBlocks = (node: FlowNode): boolean => (node.config.level || 'everyone') === 'everyone';
const alwaysPasses = (node: FlowNode): boolean => Number(node.config.percent) >= 100;

function lacksViewer(flow: Flow | undefined): boolean {
  const trigger = flow?.nodes.map((entry) => specFor(entry.type)).find((spec) => spec?.kind === 'trigger');
  if (trigger?.type === 'countdown.finished') return false;
  return !trigger?.provides?.some((variable) => variable.token === '{user.id}');
}

const CHECK_NOTE_RULES: Readonly<Record<FlowNode['type'], (node: FlowNode, flow?: Flow) => boolean>> = Object.freeze({
  'cond.role': (node: FlowNode) => neverBlocks(node),
  'cond.chance': (node: FlowNode) => alwaysPasses(node),
  'cond.follow': (_node: FlowNode, flow?: Flow) => lacksViewer(flow)
});

const CHECK_NOTE_KEYS: Readonly<Record<FlowNode['type'], MessageKey>> = Object.freeze({
  'cond.role': 'flows.step.condition.role.everyoneNote',
  'cond.chance': 'flows.step.condition.chance.alwaysNote',
  'cond.follow': 'flows.step.condition.follow.viewerNote'
});

export const hasCheckNote = (node: FlowNode): boolean => Object.hasOwn(CHECK_NOTE_RULES, node.type);

const showsCheckNote = (node: FlowNode, flow?: Flow): boolean => hasCheckNote(node) && CHECK_NOTE_RULES[node.type]!(node, flow);

export function checkNote(t: Translate, node: FlowNode | undefined, flow?: Flow): string | null {
  if (!node || !showsCheckNote(node, flow)) return null;
  return t(CHECK_NOTE_KEYS[node.type]!);
}

const known = (key: string, text: string): string => (text === key ? '' : text);

export const stepKeywords = (t: Translate, spec: NodeSpec): string =>
  known(`flows.step.${spec.kind}.${spec.slug}.keywords`, t(`flows.step.${spec.kind}.${spec.slug}.keywords` as MessageKey));

export const groupLabel = (t: Translate, group: StepGroup): string => t(`flows.board.group.${group}`);

type FieldCopy = { spec: NodeSpec; field: FieldSpec; node?: Pick<FlowNode, 'config'> };

function fieldVariant({ spec, field, node }: FieldCopy): string | undefined {
  if (!field.labelBy || !node) return undefined;
  const select = spec.fields.find((entry) => entry.key === field.labelBy);
  if (select?.type !== 'select') return undefined;
  return String(node.config[select.key] ?? select.options[0] ?? '') || undefined;
}

const fieldCopyPrefix = (spec: NodeSpec, field: FieldSpec) => `flows.step.${spec.kind}.${spec.slug}.field.${field.key}`;

function variantCopy(t: Translate, source: FieldCopy, part: 'label' | 'placeholder'): string | undefined {
  const variant = fieldVariant(source);
  if (!variant) return undefined;
  const prefix = fieldCopyPrefix(source.spec, source.field);
  return known(`${prefix}.${variant}.${part}`, t(`${prefix}.${variant}.${part}` as MessageKey)) || undefined;
}

export function fieldLabel(t: Translate, spec: NodeSpec, field: FieldSpec, node?: FieldCopy['node']): string {
  if (field.type === 'target') return t('flows.step.target.label');
  if (field.key === 'login' && targetFieldFor(spec)) return t('flows.step.target.viewerLabel');
  if (field.key === 'target' && targetFieldFor(spec)) return t('flows.step.target.loginLabel');
  return variantCopy(t, { spec, field, node }, 'label') ?? t(`${fieldCopyPrefix(spec, field)}.label` as MessageKey);
}

export const targetOptions = (t: Translate, modes: readonly string[]): { value: string; label: string }[] =>
  modes.map((value) => ({ value, label: t(`flows.step.target.option.${value}` as MessageKey) }));

export const fieldHint = (t: Translate, spec: NodeSpec, field: FieldSpec): string | undefined =>
  known(`${fieldCopyPrefix(spec, field)}.hint`, t(`flows.step.${spec.kind}.${spec.slug}.field.${field.key}.hint` as MessageKey)) || undefined;

export function fieldIssueMessage(t: Translate, issue: FieldIssue, node: FlowNode | undefined): string {
  if (issue.code !== 'required') return issueMessage(t, issue);
  if (node?.type === 'reply.reward_update' && issue.fieldKey === 'setEnabled') return t('flows.step.reply.rewardUpdate.pickChange');
  const spec = node ? specFor(node.type) : undefined;
  const field = spec?.fields.find((field) => field.key === issue.fieldKey);
  if (!spec || !field) return issueMessage(t, issue);
  return t('flows.issue.requiredField', { field: fieldLabel(t, spec, field, node) });
}

export function fieldPlaceholder(t: Translate, spec: NodeSpec, field: FieldSpec, node?: FieldCopy['node']): string | undefined {
  const variant = variantCopy(t, { spec, field, node }, 'placeholder');
  if (variant) return variant;
  if (!('placeholder' in field) || !field.placeholder) return undefined;
  return t(`${fieldCopyPrefix(spec, field)}.placeholder` as MessageKey);
}

export function fieldUnit(t: Translate, spec: NodeSpec, field: FieldSpec): string | undefined {
  if (field.type !== 'number' || !field.unit) return undefined;
  return t(`flows.step.${spec.kind}.${spec.slug}.field.${field.key}.unit` as MessageKey, { max: MAX_WAIT_SECONDS });
}

export type DurationSpec = Extract<FieldSpec, { type: 'duration' }>;

export const durationRange = (t: Translate, spec: NodeSpec, field: DurationSpec): string =>
  t(`flows.step.${spec.kind}.${spec.slug}.field.${field.key}.range` as MessageKey, { days: field.max / 86_400, max: field.max });

export function durationOutside(field: DurationSpec, seconds: string | number | boolean | undefined): boolean {
  if (seconds === undefined) return false;
  return Number(seconds) < field.min || Number(seconds) > field.max;
}

function optionKey(spec: NodeSpec, field: FieldSpec, value: string): MessageKey {
  const shared = field.type === 'select' ? field.optionKey : undefined;
  if (typeof shared === 'string') return `${shared}.${value}`;
  return shared?.[value] ?? `${fieldCopyPrefix(spec, field)}.option.${value}`;
}

export const optionText = (t: Translate, spec: NodeSpec, field: FieldSpec, value: string): string =>
  t(optionKey(spec, field, value));

export function fieldOptions(t: Translate, spec: NodeSpec, field: FieldSpec): { value: string; label: string }[] {
  if (field.type !== 'select') return [];
  return field.options.map((value) => ({ value, label: optionText(t, spec, field, value) }));
}

function variantLabels(t: Translate, spec: NodeSpec, field: FieldSpec): string[] {
  const control = spec.fields.find((entry) => entry.key === field.labelBy);
  const values = control?.type === 'select' ? control.options : [];
  return values.map((value) => variantCopy(t, { spec, field, node: { config: { [control!.key]: value } } }, 'label') ?? '');
}

export function fieldSearchText(t: Translate, spec: NodeSpec): string {
  return spec.fields.flatMap((field) => {
    const prefix = fieldCopyPrefix(spec, field);
    const options = field.type === 'select' ? field.options : [];
    return [
      known(`${prefix}.label`, t(`${prefix}.label` as MessageKey)),
      ...variantLabels(t, spec, field),
      ...options.map((value) => {
        const key = optionKey(spec, field, value);
        return known(key, t(key));
      })
    ];
  }).filter(Boolean).join(' ');
}

export const outputLabel = (t: Translate, spec: NodeSpec, id: string): string =>
  outputsFor(spec).some((output) => output.id === id && id !== 'next')
    ? t(`flows.step.${spec.kind}.${spec.slug}.out.${id}` as MessageKey) : t('flows.step.then');

export const randomPathLabel = (t: Translate, path: number | string): string => t('flows.step.reply.random.path', { path });

export const variableLabel = (t: Translate, variable: VariableSpec): string => variable.literalName ? variable.name : t(`flows.step.variable.${variable.name}` as MessageKey);
