// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { MessageKey, RehearsedLine } from '@bagel/kit';
import { compactDuration, compileFlow, defaultScenario, rehearseDefinition, SCENARIO_FIELDS, scenarioIdentity, scenarioKind, scenarioPatches, validateDefinition } from '@bagel/kit/automations';
import type { Definition, Issue, RehearsalEvent, RehearsalOverrides, RehearsalScenario, ScenarioField } from '@bagel/kit/automations';
export type { RehearsalOverrides, RehearsalScenario, ScenarioField, ScenarioValue } from '@bagel/kit/automations';
import { fieldLabel, stepLabel } from './copy';
import { describe } from './describe';
import { issueMessage, stepOf, type Translate } from './messages';
import { specFor, type Flow, type FlowNode } from './model';

type Text = string;
type ScenarioPatch = Partial<RehearsalScenario>;
type Compiled = Extract<ReturnType<typeof compileFlow>, { ok: true }>['value'];
export type RehearsalStep = RehearsalEvent & {
  id: Text | null;
  label: Text;
  text: Text;
  chatLines: RehearsedLine[];
  missing: Text[];
  sample: boolean;
  secondary?: Text;
  node?: FlowNode;
  repair?: ScenarioPatch;
};
export type RehearsalView = {
  steps: RehearsalStep[];
  scenario: RehearsalScenario;
  fields: readonly ScenarioField[];
  summary: Text;
  starts: boolean;
  blocked?: Text;
  startPatch?: ScenarioPatch;
  issue?: { text: Text; node: Text | null };
};
export type RehearsalOptions = { scenario?: ScenarioPatch; overrides?: RehearsalOverrides };

const ROLE_LABELS: Readonly<Record<Text, MessageKey>> = {
  viewer: 'flows.test.role.viewer', everyone: 'flows.test.role.viewer', sub: 'flows.test.role.sub', vip: 'flows.test.role.vip',
  mod: 'flows.test.role.mod', lead_mod: 'flows.test.role.lead_mod', broadcaster: 'flows.test.role.broadcaster'
};
const FIELD_LABELS: Readonly<Record<Text, MessageKey>> = {
  user: 'flows.test.field.user', userId: 'flows.test.field.userId', role: 'flows.test.field.role', live: 'flows.test.field.live',
  message: 'flows.test.field.message', bits: 'flows.test.field.bits', amount: 'flows.test.field.amount', viewers: 'flows.test.field.viewers',
  tier: 'flows.test.field.tier', months: 'flows.test.field.months', giftCount: 'flows.test.field.giftCount', anonymous: 'flows.test.field.anonymous',
  input: 'flows.test.field.input', requestText: 'flows.test.field.requestText', isGift: 'flows.test.field.isGift',
  rewardId: 'flows.test.field.rewardId', reward_id: 'flows.test.field.rewardId', name: 'flows.test.field.name', is_gift: 'flows.test.field.isGift'
};

export const scenarioFieldLabel = (t: Translate, key: Text): Text => t(FIELD_LABELS[key] ?? 'flows.test.notSet');
export const scenarioInputLabel = (t: Translate, key: Text): Text => {
  const labels: Readonly<Record<Text, MessageKey>> = { bits: 'flows.test.inputBits', amount: 'flows.test.inputBits', viewers: 'flows.test.inputViewers' };
  return labels[key] ? t(labels[key]) : scenarioFieldLabel(t, key);
};
export const scenarioRoleLabel = (t: Translate, key: Text): Text => t(ROLE_LABELS[key] ?? 'flows.test.role.viewer');

function repairDraft(flow: Flow, issues: Issue[]): Flow {
  const loose = new Set(issues.filter((issue) => issue.code === 'unconnected_node').map((issue) => stepOf([issue], flow.nodes)));
  const nodes = flow.nodes.filter((node) => !loose.has(node.id)).map((node) => ({ ...node, config: { ...node.config } }));
  for (const issue of issues) repairField(nodes, issue);
  const known = new Set(nodes.map((node) => node.id));
  return { ...flow, nodes, edges: flow.edges.filter((edge) => known.has(edge.from) && known.has(edge.to)) };
}

function repairField(nodes: FlowNode[], issue: Issue): void {
  const node = nodes.find((node) => node.id === stepOf([issue], nodes));
  if (!node) return;
  if (issue.code === 'user_not_resolved') node.config.userId = '0';
  if (issue.code !== 'unmappable_token') return;
  const [, , field, token] = issue.path.split('.');
  node.config[field!] = String(node.config[field!] ?? '').replaceAll(`{${token}}`, `{unset:${token}}`);
}

function compileDraft(flow: Flow): ReturnType<typeof compileFlow> {
  const compiled = compileFlow(flow);
  if (compiled.ok) return compiled;
  const tolerated = new Set(['unconnected_node', 'user_not_resolved', 'unmappable_token']);
  if (!compiled.issues.every((issue) => tolerated.has(issue.code))) return compiled;
  return compileDraft(repairDraft(flow, compiled.issues));
}

function initialScenario(definition: Definition): RehearsalScenario {
  const scenario = defaultScenario(definition);
  const fields = SCENARIO_FIELDS[scenarioKind(definition)] ?? [];
  for (const field of fields.filter((field) => field.key === 'bits' || field.key === 'viewers')) Object.assign(scenario, { [field.key]: field.default });
  return scenario;
}

function executorOverrides(compiled: Compiled, overrides: RehearsalOverrides): RehearsalOverrides {
  const entries = [...compiled.nodeByPath].flatMap(([path, id]) => {
    const key = path.endsWith('.params.condition') ? path.slice(0, -17) : path;
    return ['', '.response', '.error'].filter((suffix) => overrides[`${id}${suffix}`] !== undefined)
      .map((suffix) => [`${key}${suffix}`, overrides[`${id}${suffix}`]!] as const);
  });
  return Object.fromEntries(entries);
}

const FILTER_FIELDS: Readonly<Record<Text, keyof RehearsalScenario>> = { amount: 'bits', viewers: 'viewers', reward_id: 'rewardId', name: 'name', is_gift: 'isGift', tier: 'tier', anonymous: 'anonymous' };

function startPatch(reason: { reason: Text; values: Record<Text, Text> }, definition: Definition): ScenarioPatch | undefined {
  if (reason.reason === 'trigger_message') return { message: defaultScenario(definition).message };
  if (reason.reason !== 'trigger_filter') return undefined;
  const field = FILTER_FIELDS[reason.values.field!];
  if (!field) return undefined;
  const numeric = field === 'bits' || field === 'viewers';
  const value = numeric ? Number(reason.values.expected) : reason.values.expected;
  return { [field]: field === 'isGift' || field === 'anonymous' ? value === 'true' : value };
}

function blockedText(t: Translate, reason: { reason: Text; values: Record<Text, Text> }): Text {
  if (reason.reason === 'condition_failed') return t('flows.test.checkStopped');
  if (reason.reason === 'trigger_message') return t('flows.test.messageMismatch', reason.values);
  const numeric = reason.values.field === 'amount' || reason.values.field === 'viewers';
  return t(numeric ? 'flows.test.below' : 'flows.test.filterMismatch', { ...reason.values, field: scenarioFieldLabel(t, reason.values.field ?? '') });
}

export function scenarioSummary(t: Translate, scenario: RehearsalScenario, kind: Text): Text {
  const user = scenario.anonymous ? t('flows.test.anonymousViewer') : scenario.user || t('flows.test.notSet');
  if (kind.startsWith('twitch.chat.')) return t('flows.test.summaryChat', { user, role: scenarioRoleLabel(t, scenario.role ?? 'viewer'), message: scenario.message ?? '' });
  const key = kind.replace('twitch.', '').replaceAll('.', '_');
  const summaries: Record<Text, Text> = {
    follow: t('flows.test.summaryFollow', { user }),
    cheer: t('flows.test.summaryCheer', { user, count: scenario.bits ?? 0 }),
    raid: t('flows.test.summaryRaid', { user, count: scenario.viewers ?? 0 }),
    sub_new: t('flows.test.summarySub', { user, tier: Number(scenario.tier ?? 1000) / 1000 }),
    sub_resub: t('flows.test.summaryResub', { user, months: scenario.months ?? 0 }),
    sub_gift: t('flows.test.summaryGift', { user, count: scenario.giftCount ?? 0 }),
    reward: t('flows.test.summaryReward', { user, input: scenario.input ?? '' }),
    unban_request: t('flows.test.summaryUnban', { user, text: scenario.requestText ?? '' }),
    stream_online: t('flows.test.summaryLive'),
    stream_offline: t('flows.line.eventOffline'),
    countdown_finished: t('flows.test.summaryCountdown', { name: scenario.name ?? '' })
  };
  return summaries[key] ?? t('flows.test.summaryEvent', { user });
}

const INPUT_TOKENS: Readonly<Record<Text, keyof RehearsalScenario>> = { user: 'user', 'user.id': 'userId', userid: 'userId', message: 'message', bits: 'bits', 'event.amount': 'bits', viewers: 'viewers', 'event.viewers': 'viewers', 'event.months': 'months', months: 'months', 'event.input': 'input', input: 'input' };

function compareRepair(node: FlowNode): ScenarioPatch | undefined {
  const token = /^\{([^}]+)\}$/.exec(String(node.config.left));
  const field = INPUT_TOKENS[token?.[1] ?? ''];
  if (!field) return undefined;
  const right = String(node.config.right ?? '');
  if (right.includes('{')) return undefined;
  const value = node.config.op === 'lt' ? String(Number(right) - 1) : right;
  return { [field]: ['bits', 'viewers', 'months'].includes(field) ? Number(value) : value };
}

function checkRepair(node: FlowNode, event: RehearsalEvent): ScenarioPatch | undefined {
  if (event.status !== 'no') return undefined;
  if (node.type === 'cond.role') return { role: node.config.level as RehearsalScenario['role'] };
  if (node.type === 'cond.user') return node.config.userId ? { userId: String(node.config.userId), user: String(node.config.login) } : undefined;
  return node.type === 'cond.compare' ? compareRepair(node) : undefined;
}

function checkText(t: Translate, node: FlowNode, scenario: RehearsalScenario): Text {
  if (node.type === 'cond.role') return t('flows.test.roleCheck', { actual: scenarioRoleLabel(t, scenario.role ?? 'viewer'), expected: scenarioRoleLabel(t, String(node.config.level ?? 'viewer')) });
  if (node.type === 'cond.user') return t('flows.test.userCheck', { actual: scenario.userId ?? t('flows.test.notSet'), expected: String(node.config.userId || t('flows.test.notSet')) });
  return describe(t, { ...node, config: { ...node.config, left: displayInput(node.config.left, scenario), right: displayInput(node.config.right, scenario) } });
}

function displayInput(value: FlowNode['config'][Text] | undefined, scenario: RehearsalScenario): Text {
  return String(value ?? '').replace(/\{([^}]+)\}/g, (token, key: Text) => {
    const field = INPUT_TOKENS[key];
    return field ? String(scenario[field] ?? token) : token;
  });
}

function loopText(t: Translate, event: RehearsalEvent): Text {
  return event.port === 'done' ? t('flows.test.loopDone') : t('flows.test.loop', { count: event.detail?.times ?? '' });
}

function triggerText(node: FlowNode | undefined, scenario: RehearsalScenario, line: Text, t: Translate): Text {
  if (node?.type === 'twitch.stream.offline') return t('flows.line.eventOffline');
  return node?.type.startsWith('twitch.chat.') ? scenario.message ?? line : line;
}

function effectText(t: Translate, event: RehearsalEvent, node: FlowNode | undefined): Text {
  if (!node) return t('flows.test.effect');
  if (node.type === 'reply.math' && event.detail?.value !== undefined) return event.detail.value;
  const config = { ...node.config, target: event.detail?.login ?? node.config.target };
  return describe(t, { ...node, config });
}

function eventText(t: Translate, event: RehearsalEvent, node: FlowNode | undefined, scenario: RehearsalScenario): Text {
  const line = effectText(t, event, node);
  const detail = event.detail ?? {};
  const texts = {
    trigger: triggerText(node, scenario, line, t), parallel: '', loop: loopText(t, event),
    wait: t('flows.test.wait', { seconds: Number(detail.ms) / 1000 }),
    check: node ? checkText(t, node, scenario) : line, effect: line, say: '', skip: line
  };
  return detail.code ? reasonText(t, detail.code) : texts[event.kind];
}

function chatLines(event: RehearsalEvent): RehearsedLine[] {
  const mode = event.detail?.mode === 'announce' ? 'announce' : 'chat';
  return (event.lines ?? []).map((text) => ({ mode, color: event.detail?.color, segments: [{ text, kind: 'plain' }] }));
}

function reasonText(t: Translate, code: Text): Text {
  const reasons: Record<Text, Text> = {
    disabled: t('flows.test.disabled'), empty: t('flows.board.nothingToSend'), offline: t('flows.test.offline'),
    no_viewer: t('flows.test.noViewer'), bad_times: t('flows.test.badTimes'), delay_budget: t('flows.issue.delayBudget'),
    max_steps: t('flows.test.limit'), max_actions: t('flows.test.limit'), upstream_error: t('flows.test.apiFailed'),
    not_running: t('flows.test.notRunning'), below_rate: t('flows.test.belowRate'), var_limit: t('flows.test.varLimit'),
    text_error: t('flows.test.textError'), math_error: t('flows.test.mathError'), bad_def: t('flows.test.badDef'),
    empty_login: t('flows.test.noTarget'), perm_required: t('flows.issue.permRequired')
  };
  return reasons[code] ?? t('flows.test.failed');
}

type RunContext = {
  compiled: Compiled; nodes: Map<Text, FlowNode>; scenario: RehearsalScenario; overrides: RehearsalOverrides;
  repairs: Map<Text, ScenarioPatch | undefined>; formatDuration: (seconds: number) => Text;
};

const REQUIRED_FIELDS: Readonly<Record<Text, readonly Text[]>> = {
  'twitch.chat.command': ['trigger'], 'twitch.chat.word': ['word'], 'reply.chat': ['message'],
  'cond.role': ['level'], 'cond.compare': ['left', 'right'], 'cond.var': ['name'],
  'reply.http_get': ['url'], 'reply.http_post': ['url'], 'reply.loop': ['times'],
  'reply.shoutout': ['target'], 'reply.timeout': ['target']
};

function unsetFields(t: Translate, node: FlowNode | undefined): Text[] {
  const spec = node ? specFor(node.type) : undefined;
  if (!node || !spec) return [];
  const required = REQUIRED_FIELDS[node.type] ?? [];
  return spec.fields.filter((field) => required.includes(field.key)).filter((field) => !String(node.config[field.key] ?? '').trim())
    .map((field) => fieldLabel(t, spec, field));
}

function verifiedRepair(node: FlowNode | undefined, event: RehearsalEvent, context: RunContext): ScenarioPatch | undefined {
  const repair = node ? checkRepair(node, event) : undefined;
  if (!repair) return undefined;
  const key = `${event.path}:${JSON.stringify(repair)}`;
  if (context.repairs.has(key)) return context.repairs.get(key);
  const result = rehearseDefinition(context.compiled.definition, { ...context.scenario, ...repair }, context.overrides, context.formatDuration);
  const verified = result.events.some((candidate) => candidate.path === event.path && candidate.status === 'yes') ? repair : undefined;
  context.repairs.set(key, verified);
  return verified;
}

function mappedStep(t: Translate, event: RehearsalEvent, context: RunContext): RehearsalStep {
  const { compiled, nodes, scenario } = context;
  const id = compiled.nodeByPath.get(`${event.path}.params.condition`) ?? compiled.nodeByPath.get(event.path) ?? null;
  const node = nodes.get(id ?? '');
  const spec = node ? specFor(node.type) : undefined;
  return { ...event, ...stepValues(t, event, node), id, node, chatLines: chatLines(event),
    secondary: node?.type === 'reply.math' ? String(node.config.expression ?? '') : undefined,
    label: spec ? stepLabel(t, spec) : t('flows.board.atSameTime'),
    text: eventText(t, event, node, scenarioIdentity(compiled.definition, scenario)), repair: verifiedRepair(node, event, context) };
}

function stepValues(t: Translate, event: RehearsalEvent, node: FlowNode | undefined): Pick<RehearsalStep, 'missing' | 'sample'> {
  const tokens = (event.detail?.missing ?? '').split(',').filter(Boolean).map((token) => token.replace(/^unset:/, ''));
  const missing = [...new Set([...tokens, ...unsetFields(t, node)])];
  const sample = event.detail?.random === '1' || Object.values(node?.config ?? {}).some((value) => /\{(?:random|choice)[}:]/.test(String(value)));
  return { missing, sample };
}

function permissionWarnings(definition: Definition): RehearsalEvent[] {
  return validateDefinition(definition).filter((issue) => issue.code === 'perm_required').map((issue) => ({
    key: `warning:${issue.path}`, path: issue.path.replace(/\.type$/, ''), lane: [], iteration: [], atMs: 0,
    kind: 'effect', status: 'failed', detail: { code: issue.code }
  }));
}

function durationFormatter(t: Translate): (seconds: number) => Text {
  const units = [t('flows.test.duration.day'), t('flows.test.duration.hour'), t('flows.test.duration.minute'), t('flows.test.duration.second')];
  return (seconds) => compactDuration(seconds, units);
}

export function rehearseFlow(flow: Flow, t: Translate, options: RehearsalOptions = {}): RehearsalView {
  const compiled = compileDraft(flow);
  const empty: RehearsalView = { steps: [], fields: [], scenario: { live: true, ...options.scenario }, summary: '', starts: false };
  if (!compiled.ok) return { ...empty, issue: { text: issueMessage(t, compiled.issues[0]!), node: stepOf(compiled.issues, flow.nodes) } };
  const { definition } = compiled.value;
  const kind = scenarioKind(definition);
  const fields = SCENARIO_FIELDS[kind] ?? [];
  const scenario = { ...initialScenario(definition), ...scenarioPatches(fields, options.scenario ?? {}) };
  const overrides = executorOverrides(compiled.value, options.overrides ?? {});
  const formatDuration = durationFormatter(t);
  const result = rehearseDefinition(definition, scenario, overrides, formatDuration);
  const context: RunContext = { compiled: compiled.value, nodes: new Map(flow.nodes.map((node) => [node.id, node])), scenario, overrides, repairs: new Map(), formatDuration };
  return { scenario, fields, summary: scenarioSummary(t, scenario, kind), starts: result.starts,
    steps: [...permissionWarnings(definition), ...result.events].map((event) => mappedStep(t, event, context)),
    blocked: result.reason ? blockedText(t, result.reason) : undefined,
    startPatch: result.reason ? startPatch(result.reason, definition) : undefined };
}

export function setStoredValue(scenario: ScenarioPatch, node: FlowNode, value: Text): ScenarioPatch {
  const key = node.config.scope === 'viewer' ? 'viewerVars' : 'channelVars';
  const name = String(node.config.name ?? '').toLowerCase();
  return { state: { ...scenario.state, [key]: { ...scenario.state?.[key], [name]: value } } };
}
