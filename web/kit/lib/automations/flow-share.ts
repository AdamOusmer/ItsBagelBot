// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { lex } from '../engine/tmpl';
import { classifyFlow, type AlertKey, type SimpleCommand } from './flow-layer';
import { CODE, MAX_AUTOMATIONS_PER_CHANNEL, MAX_DEFINITION_BYTES, MAX_FLOWS_PER_FILE, MAX_FLOW_FILE_BYTES, MAX_NAME_LENGTH } from './limits';
import { byteLength, trimSpace } from './text';
import { isSetupValue, SETUP_FILTER_VALUE, SETUP_LOGIN, SETUP_REWARD_ID, SETUP_RUN_TARGET, SETUP_SCENE_VALUE, SETUP_TEXT, SETUP_URL } from './flow-setup';
import { scrubSecrets, type ScrubKind } from './flow-share-scrub';
import type { Action, Condition, Definition, Issue, Params } from './types';
import { validateDefinition, validateName } from './validate';

export * from './flow-setup';

export const FLOW_FILE_FORMAT = 'itsbagelbot.flows';
export const FLOW_FILE_VERSION = 1;

/** One recipe in a file. `ref` only links `run` steps inside the same file. */
export interface FlowFileEntry {
  ref: string;
  name: string;
  definition: Definition;
}

/** The whole file. No ids, revisions, timestamps, usage, previews or enabled state. */
export interface FlowFile {
  format: typeof FLOW_FILE_FORMAT;
  version: typeof FLOW_FILE_VERSION;
  flows: FlowFileEntry[];
}

export type SetupCode =
  | 'reward_trigger'
  | 'event_filter'
  | 'users_list'
  | 'login_target'
  | 'http_target'
  | 'reward_target'
  | 'overlay_target'
  | 'lights_target'
  | 'slash_command'
  | 'secret_removed'
  | 'run_removed'
  | 'run_target_simple'
  | 'run_linked'
  | 'link_failed'
  | 'counter'
  | 'fetch'
  | 'command_in_use'
  | 'privileged_action';

const REVIEW_ONLY: ReadonlySet<SetupCode> = new Set([
  'run_linked',
  'counter',
  'fetch',
  'command_in_use',
  'privileged_action'
]);

export interface SetupNote {
  code: SetupCode;
  path: string;
  detail?: string;
}

export const needsSetup = (note: SetupNote): boolean => !REVIEW_ONLY.has(note.code);

export interface FlowExportRow {
  id?: string;
  name: string;
  definition: Definition;
  restricted?: boolean;
}

/** What a secret scan took out of a recipe. Holds where it was, never the removed text. */
export interface ScrubNote {
  path: string;
  type: string;
  kind: ScrubKind;
}

export interface FlowExportReport {
  name: string;
  setup: SetupNote[];
  scrubbed: ScrubNote[];
  issues: Issue[];
}

export interface FlowExport {
  file: FlowFile;
  report: FlowExportReport[];
}

export interface FlowImportContext {
  count: number;
  names: string[];
  commands: string[];
  alerts: string[];
}

export type FlowImportStatus = 'ready' | 'invalid' | 'conflict' | 'over_limit' | 'skipped';

export type FlowLanding = 'automation' | 'command' | 'alert';

export interface FlowImportEntry {
  index: number;
  sourceName: string;
  name: string;
  renamed: boolean;
  status: FlowImportStatus;
  landing?: FlowLanding;
  target?: string;
  issues: Issue[];
  setup: SetupNote[];
  ref?: string;
  definition?: Definition;
}

export type FlowImportPlan = { ok: true; entries: FlowImportEntry[]; slots: number } | { ok: false; issues: Issue[] };

export type FlowImportEntryView = Omit<FlowImportEntry, 'definition' | 'ref'> & { review: SetupNote[] };

export type FlowImportResultStatus = 'created' | Exclude<FlowImportStatus, 'ready'> | 'failed';

export interface FlowImportResult {
  index: number;
  sourceName: string;
  name: string;
  renamed: boolean;
  status: FlowImportResultStatus;
  landing?: FlowLanding;
  target?: string;
  id?: string;
  issues: Issue[];
  setup: SetupNote[];
}

export type FlowImportResultView = FlowImportResult & { review: SetupNote[] };

export interface FlowWrite {
  id?: string;
  expectedRevision?: number;
  name: string;
  enabled: false;
  definition: Definition;
  preview?: string;
}

export type FlowWriteResult = { ok: true; id?: string; revision?: number } | { ok: false; issues: Issue[] };

export interface FlowCommitDeps {
  write: (input: FlowWrite) => Promise<FlowWriteResult>;
  preview: (definition: Definition, name: string) => string | undefined;
}

const REF_PATTERN = /^f[0-9]{1,3}$/;
const ENTRY_KEYS = new Set(['ref', 'name', 'definition']);
const FILE_KEYS = new Set(['format', 'version', 'flows']);
const SAFE_KEY = /^[a-z0-9_]+$/;
const FORBIDDEN_KEYS = new Set(['__proto__', 'constructor', 'prototype']);
const FILTERS_WITH_CHANNEL_VALUES = new Set(['user', 'login', 'user_id', 'reward', 'reward_id']);
const LOGIN_TARGET_ACTIONS = new Set(['timeout', 'ban', 'warn', 'shoutout', 'points_add', 'points_set']);
const CHAT_TEXT_ACTIONS = new Set(['chat', 'reply', 'announce']);
const PRIVILEGED_ACTIONS = new Set([
  'ban',
  'timeout',
  'warn',
  'commercial',
  'shield',
  'delete_message',
  'points_add',
  'points_set',
  'set_title',
  'set_game',
  'set_tags'
]);
// Types whose params are rewritten by a rule below. Any type in neither list has its params dropped on the way through.
export const SHARE_REWRITTEN = {
  trigger: ['reward', 'event'],
  condition: ['users'],
  action: ['lights', 'http_post', 'http_get', 'reward_update', 'overlay_show', 'overlay_sound', 'run']
} as const;
// Types reviewed as carrying only the user's own text, numbers and names. A new type is not shareable until it is added here.
export const SHARE_REVIEWED = {
  trigger: ['command', 'timer', 'manual'],
  condition: ['perm', 'live', 'cooldown', 'chance', 'counter', 'points', 'arg_regex', 'category', 'compare', 'var'],
  action: [
    'chat',
    'announce',
    'reply',
    'timeout',
    'ban',
    'delete_message',
    'warn',
    'shield',
    'shoutout',
    'clip',
    'marker',
    'set_title',
    'set_game',
    'set_tags',
    'commercial',
    'points_add',
    'points_set',
    'counter_add',
    'counter_set',
    'redemption',
    'delay',
    'random_pick',
    'parallel',
    'if',
    'stop',
    'var_set',
    'var_add',
    'var_delete',
    'text',
    'math',
    'loop'
  ]
} as const;
const SHAREABLE: Record<keyof typeof SHARE_REVIEWED, ReadonlySet<string>> = {
  trigger: new Set([...SHARE_REWRITTEN.trigger, ...SHARE_REVIEWED.trigger]),
  condition: new Set([...SHARE_REWRITTEN.condition, ...SHARE_REVIEWED.condition]),
  action: new Set([...SHARE_REWRITTEN.action, ...SHARE_REVIEWED.action])
};
const REFERENCE_TOKENS: Record<string, SetupCode> = {
  count: 'counter',
  counter: 'counter',
  urlfetch: 'fetch'
};
// The same check flow-store refuses on save: automations have no use counter.
const USES_TOKEN = /\{(count|uses)\}/;
const SLASH_TEXT = /^\s*\/|\{choice:[^}]*\//i;
const SLASH_VERB = /\/[a-z]{1,32}/i;
const LOGIN_DECORATION = /[@\s]/g;

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const list = <T>(v: unknown): T[] => (Array.isArray(v) ? (v as T[]) : []);

type Notes = SetupNote[];
type ActionVisit = (action: Action, path: string) => Action | null;

function mapBranches(params: Params, path: string, visit: ActionVisit): Params {
  const branches = list<Record<string, unknown>>(params.branches).map((branch, i) => ({
    ...branch,
    actions: mapActions(list<Action>(branch.actions), `${path}.params.branches[${i}].actions`, visit)
  }));
  return { ...params, branches };
}

function mapArms(params: Params, arms: readonly string[], path: string, visit: ActionVisit): Params {
  const out: Params = { ...params };
  for (const arm of arms) {
    if (params[arm]) out[arm] = mapActions(list<Action>(params[arm]), `${path}.params.${arm}`, visit);
  }
  return out;
}

const API_ARMS = ['on_ok', 'on_failure'];

const NAMED_ARMS = new Map<string, readonly string[]>([
  ['if', ['then', 'else']],
  ['loop', ['actions']],
  ['http_get', API_ARMS],
  ['http_post', API_ARMS]
]);

function mapNested(action: Action, path: string, visit: ActionVisit): Action {
  const params = action.params ?? {};
  const arms = NAMED_ARMS.get(action.type);
  if (arms) return { ...action, params: mapArms(params, arms, path, visit) };
  if (action.type === 'random_pick' || action.type === 'parallel') return { ...action, params: mapBranches(params, path, visit) };
  return action;
}

function mapActions(actions: Action[], path: string, visit: ActionVisit): Action[] {
  return actions.flatMap((action, i) => {
    const at = `${path}[${i}]`;
    const kept = visit(action, at);
    return kept ? [mapNested(kept, at, visit)] : [];
  });
}

function mapDefinitionActions(definition: Definition, visit: ActionVisit): Definition {
  return { ...definition, actions: mapActions(list<Action>(definition.actions), 'actions', visit) };
}

function neutralUsers(params: Params): Params {
  const hadAllow = list(params.allow).length + list(params.allow_ids).length > 0;
  return hadAllow ? { allow: [SETUP_LOGIN] } : { deny: [SETUP_LOGIN] };
}

function neutralCondition(condition: Condition, path: string, notes: Notes): Condition {
  if (!SHAREABLE.condition.has(condition.type)) return { ...condition, params: {} };
  const params = condition.params ?? {};
  if (condition.type === 'users') {
    notes.push({ code: 'users_list', path });
    return { ...condition, params: neutralUsers(params) };
  }
  if (condition.type === 'counter') notes.push({ code: 'counter', path, detail: str(params.name) });
  return condition;
}

function neutralFilters(filters: Record<string, string>, path: string, notes: Notes): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(filters)) {
    const channelBound = FILTERS_WITH_CHANNEL_VALUES.has(key);
    if (channelBound) notes.push({ code: 'event_filter', path: `${path}.${key}` });
    out[key] = channelBound ? SETUP_FILTER_VALUE : value;
  }
  return out;
}

function neutralTrigger(definition: Definition, notes: Notes): Definition {
  const { trigger } = definition;
  const params = trigger.params ?? {};
  if (!SHAREABLE.trigger.has(trigger.type)) return { ...definition, trigger: { ...trigger, params: {} } };
  if (trigger.type === 'reward') {
    notes.push({ code: 'reward_trigger', path: 'trigger.params.reward_id' });
    return { ...definition, trigger: { ...trigger, params: { reward_id: SETUP_REWARD_ID } } };
  }
  if (trigger.type !== 'event' || !params.filters) return definition;
  const filters = neutralFilters(params.filters as Record<string, string>, 'trigger.params.filters', notes);
  return { ...definition, trigger: { ...trigger, params: { ...params, filters } } };
}

function hasLiteralLogin(login: string): boolean {
  return lex(login)
    .filter((token) => token.kind === 'literal')
    .some((token) => (token.kind === 'literal' ? token.text.replace(LOGIN_DECORATION, '') !== '' : false));
}

const disabled = (action: Action, params: Params): Action => ({ ...action, enabled: false, params });

function neutralLogin(action: Action, path: string, notes: Notes): Action {
  const params = action.params ?? {};
  if (!LOGIN_TARGET_ACTIONS.has(action.type) || !hasLiteralLogin(str(params.login))) return action;
  notes.push({ code: 'login_target', path: `${path}.params.login` });
  return disabled(action, { ...params, login: SETUP_LOGIN });
}

const LIGHTS_IDENTITY_FIELDS = ['device', 'sku', 'device_name'] as const;
const LIGHTS_SCENE_FIELDS = ['scene_id', 'scene_name', 'scene_type', 'scene_instance', 'scene_value'] as const;

function neutralLights(params: Params): Params {
  const out: Params = { ...params };
  for (const key of LIGHTS_IDENTITY_FIELDS) out[key] = SETUP_TEXT;
  for (const key of LIGHTS_SCENE_FIELDS) if (str(params[key]) !== '') out[key] = SETUP_TEXT;
  if (out.scene_value === SETUP_TEXT) out.scene_value = SETUP_SCENE_VALUE;
  return out;
}

const NESTED_API_FIELDS = ['save_as', 'on_ok', 'on_failure'] as const;

function apiPaths(params: Params): Params {
  const out: Params = {};
  for (const key of NESTED_API_FIELDS) if (params[key] !== undefined) out[key] = params[key];
  return out;
}

const NEUTRAL_MEDIA = new Set(['image', 'video']);

function neutralHttpGet(params: Params): Params {
  const path = list(params.json_path).length > 0 ? { json_path: params.json_path } : {};
  return { url: SETUP_URL, ...path, ...apiPaths(params) };
}

function neutralRewardUpdate(params: Params): Params {
  return { reward_id: SETUP_REWARD_ID, enabled: params.enabled ?? null, cost: params.cost ?? null, reset_on_stream_end: params.reset_on_stream_end === true };
}

function neutralOverlayShow(params: Params): Params {
  const { kind, content, seconds, position } = params;
  return { kind, content: NEUTRAL_MEDIA.has(str(kind)) ? SETUP_URL : content, seconds, position };
}

type ExternalRule = { code: SetupCode; rewrite: (params: Params) => Params; applies?: (params: Params) => boolean };

const EXTERNAL_RULES = new Map<string, ExternalRule>([
  ['lights', { code: 'lights_target', rewrite: neutralLights }],
  ['http_post', { code: 'http_target', rewrite: (params) => ({ url: SETUP_URL, ...apiPaths(params) }) }],
  ['http_get', { code: 'http_target', rewrite: neutralHttpGet }],
  ['reward_update', { code: 'reward_target', rewrite: neutralRewardUpdate }],
  ['overlay_show', { code: 'overlay_target', rewrite: neutralOverlayShow, applies: (params) => NEUTRAL_MEDIA.has(str(params.kind)) }],
  ['overlay_sound', { code: 'overlay_target', rewrite: (params) => ({ url: SETUP_URL, volume: params.volume }) }]
]);

function neutralExternal(action: Action, path: string, notes: Notes): Action {
  const params = action.params ?? {};
  const rule = EXTERNAL_RULES.get(action.type);
  if (!rule || rule.applies?.(params) === false) return action;
  notes.push({ code: rule.code, path });
  return disabled(action, rule.rewrite(params));
}

function neutralSlash(action: Action, path: string, notes: Notes): Action {
  const text = str(action.params?.text);
  if (!CHAT_TEXT_ACTIONS.has(action.type) || !SLASH_TEXT.test(text)) return action;
  notes.push({ code: 'slash_command', path: `${path}.params.text`, detail: SLASH_VERB.exec(text)?.[0] });
  return disabled(action, { ...action.params, text: SETUP_TEXT });
}

const NAMED_PARAM_NOTES: Record<string, SetupCode> = {
  counter_add: 'counter',
  counter_set: 'counter'
};

function noteNamedReferences(action: Action, path: string, notes: Notes): void {
  const params = action.params ?? {};
  const named = NAMED_PARAM_NOTES[action.type];
  if (named) notes.push({ code: named, path, detail: str(params.name) });
  if (PRIVILEGED_ACTIONS.has(action.type)) notes.push({ code: 'privileged_action', path, detail: action.type });
}

function templateNotes(value: unknown, path: string): Notes {
  if (typeof value !== 'string' || !value.includes('{')) return [];
  return lex(value).flatMap((token): Notes => {
    if (token.kind !== 'var' || token.payload === null) return [];
    const code = REFERENCE_TOKENS[token.name];
    return code ? [{ code, path, detail: token.payload }] : [];
  });
}

function noteTemplateTokens(params: Params, path: string, notes: Notes): void {
  for (const [key, value] of Object.entries(params)) notes.push(...templateNotes(value, `${path}.params.${key}`));
}

function noteBlanked(action: Action, path: string, notes: Notes): void {
  if (action.enabled !== false || EXTERNAL_RULES.has(action.type)) return;
  if (Object.values(action.params ?? {}).includes(SETUP_TEXT)) notes.push({ code: 'secret_removed', path });
}

function neutralAction(action: Action, path: string, notes: Notes): Action {
  if (!SHAREABLE.action.has(action.type)) return disabled(action, {});
  noteNamedReferences(action, path, notes);
  noteTemplateTokens(action.params ?? {}, path, notes);
  noteBlanked(action, path, notes);
  const external = neutralExternal(action, path, notes);
  const withCondition = neutralIfCondition(external, path, notes);
  return neutralSlash(neutralLogin(withCondition, path, notes), path, notes);
}

function neutralIfCondition(action: Action, path: string, notes: Notes): Action {
  if (action.type !== 'if') return action;
  const params = action.params ?? {};
  const condition = neutralCondition(params.condition as Condition, `${path}.params.condition`, notes);
  return { ...action, params: { ...params, condition } };
}

type RunRule = (action: Action, path: string, notes: Notes) => Action | null;

const USER_ID_KEY = 'allowed_user_id';

const joinPath = (path: string, key: string) => (path ? `${path}.${key}` : key);

function withoutUserIds(value: unknown, path: string, notes: Notes): unknown {
  if (Array.isArray(value)) return value.map((item, i) => withoutUserIds(item, `${path}[${i}]`, notes));
  if (!isObject(value)) return value;
  const out: Record<string, unknown> = {};
  for (const [key, child] of Object.entries(value)) {
    if (key === USER_ID_KEY) notes.push({ code: 'users_list', path: joinPath(path, key) });
    else out[key] = withoutUserIds(child, joinPath(path, key), notes);
  }
  return out;
}

function neutralDefinition(source: Definition, run: RunRule): { definition: Definition; setup: Notes } {
  const notes: Notes = [];
  const definition = withoutUserIds(source, '', notes) as Definition;
  const triggered = neutralTrigger(definition, notes);
  const conditions = list<Condition>(triggered.conditions).map((c, i) => neutralCondition(c, `conditions[${i}]`, notes));
  const walked = mapDefinitionActions({ ...triggered, conditions }, (action, path) =>
    action.type === 'run' ? run(action, path, notes) : neutralAction(action, path, notes)
  );
  return { definition: compactDefinition(walked), setup: dedupeNotes(notes) };
}

function compactDefinition(definition: Definition): Definition {
  const out: Definition = { version: definition.version, trigger: definition.trigger };
  if (definition.queue) out.queue = definition.queue;
  if (list(definition.conditions).length > 0) out.conditions = definition.conditions;
  if (list(definition.actions).length > 0) out.actions = definition.actions;
  return out;
}

function dedupeNotes(notes: Notes): Notes {
  const seen = new Set<string>();
  return notes.filter((note) => {
    const key = `${note.code}\u0000${note.path}\u0000${note.detail ?? ''}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

const ARM_KEYS = new Set(['then', 'else', 'actions', 'branches', 'on_ok', 'on_failure']);

type ScrubAt = { type: string; found: ScrubNote[] };

function scrubString(text: string, path: string, at: ScrubAt): string {
  const scrubbed = scrubSecrets(text);
  for (const kind of scrubbed.kinds) at.found.push({ path, type: at.type, kind });
  return scrubbed.text;
}

function scrubValue(value: unknown, path: string, at: ScrubAt): unknown {
  if (typeof value === 'string') return scrubString(value, path, at);
  if (Array.isArray(value)) return value.map((item, i) => scrubValue(item, `${path}[${i}]`, at));
  if (!isObject(value)) return value;
  return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, scrubValue(child, `${path}.${key}`, at)]));
}

function scrubParams(params: Params, path: string, at: ScrubAt): Params {
  const out: Params = {};
  for (const [key, value] of Object.entries(params)) out[key] = ARM_KEYS.has(key) ? value : scrubValue(value, `${path}.${key}`, at);
  return out;
}

const emptiedKeys = (before: Params, after: Params) => Object.keys(before).filter((key) => str(before[key]).trim() !== '' && str(after[key]).trim() === '');

function scrubOwned<T extends { type: string; params?: Params }>(item: T, path: string, found: ScrubNote[]): T {
  return item.params ? { ...item, params: scrubParams(item.params, `${path}.params`, { type: item.type, found }) } : item;
}

function scrubAction(action: Action, path: string, found: ScrubNote[], notes: Notes): Action {
  const before = action.params;
  if (!before) return action;
  const params = scrubParams(before, `${path}.params`, { type: action.type, found });
  const emptied = emptiedKeys(before, params);
  if (emptied.length === 0) return { ...action, params };
  notes.push({ code: 'secret_removed', path });
  for (const key of emptied) params[key] = SETUP_TEXT;
  return disabled(action, params);
}

function scrubDefinition(definition: Definition, notes: Notes): { definition: Definition; scrubbed: ScrubNote[] } {
  const found: ScrubNote[] = [];
  const trigger = scrubOwned(definition.trigger, 'trigger', found);
  const conditions = list<Condition>(definition.conditions).map((condition, i) => scrubOwned(condition, `conditions[${i}]`, found));
  const walked = mapDefinitionActions({ ...definition, trigger, conditions }, (action, path) => scrubAction(action, path, found, notes));
  return { definition: compactDefinition(walked), scrubbed: found };
}

const hasRestriction = (row: FlowExportRow) => row.restricted === true || str(row.definition.trigger.params?.[USER_ID_KEY]) !== '';

// Losing the restriction would hand the importer a command anyone can fire, so it leaves as a list nobody is on yet.
function restrictedDefinition(row: FlowExportRow): Definition {
  if (!hasRestriction(row)) return row.definition;
  return { ...row.definition, conditions: [...list<Condition>(row.definition.conditions), { type: 'users', params: { allow: [SETUP_LOGIN] } }] };
}

function exportRow(row: FlowExportRow, ref: string, refs: Map<string, string>): { entry: FlowFileEntry; setup: Notes; scrubbed: ScrubNote[] } {
  const run: RunRule = (action, path, notes) => {
    const target = refs.get(str(action.params?.automation_id));
    if (target) return { ...action, params: { automation_id: target } };
    notes.push({ code: 'run_removed', path });
    return disabled(action, { automation_id: SETUP_RUN_TARGET });
  };
  const neutral = neutralDefinition(restrictedDefinition(row), run);
  const { definition, scrubbed } = scrubDefinition(neutral.definition, neutral.setup);
  const nameAt: ScrubAt = { type: 'name', found: [] };
  const name = trimSpace(scrubString(row.name, 'name', nameAt));
  return { entry: { ref, name, definition }, setup: neutral.setup, scrubbed: [...nameAt.found, ...scrubbed] };
}

function triggerGap(definition: Definition): Issue[] {
  const params = definition.trigger.params ?? {};
  const filters = isObject(params.filters) ? Object.values(params.filters) : [];
  return [...Object.values(params), ...filters].some(isSetupValue) ? [{ path: 'trigger', code: 'setup_required' }] : [];
}

function conditionGaps(definition: Definition): Issue[] {
  return list<Condition>(definition.conditions).flatMap((condition, i) =>
    condition.type === 'users' && list(condition.params?.allow).some(isSetupValue) ? [{ path: `conditions[${i}]`, code: 'setup_required' }] : []
  );
}

function actionGaps(definition: Definition): Issue[] {
  const gaps: Issue[] = [];
  mapDefinitionActions(definition, (action, path) => {
    if (Object.values(action.params ?? {}).some(isSetupValue)) gaps.push({ path, code: 'setup_required' });
    return action;
  });
  return gaps;
}

/** Steps still holding an import placeholder. A recipe with any of these cannot be turned on. */
export function setupGaps(definition: Definition): Issue[] {
  return [...triggerGap(definition), ...conditionGaps(definition), ...actionGaps(definition)];
}

function exportIssues(entry: FlowFileEntry): Issue[] {
  return [...validateName(entry.name), ...validateDefinition(entry.definition)];
}

export function exportFlows(rows: FlowExportRow[]): FlowExport {
  const refOf = (i: number) => `f${i + 1}`;
  const refs = new Map(rows.flatMap((row, i): [string, string][] => (row.id ? [[row.id, refOf(i)]] : [])));
  const report: FlowExportReport[] = [];
  const flows: FlowFileEntry[] = [];
  rows.forEach((row, i) => {
    const { entry, setup, scrubbed } = exportRow(row, refOf(i), refs);
    const issues = exportIssues(entry);
    report.push({ name: entry.name, setup: dedupeNotes(setup), scrubbed, issues });
    if (issues.length === 0) flows.push(entry);
  });
  return { file: { format: FLOW_FILE_FORMAT, version: FLOW_FILE_VERSION, flows }, report };
}

export function serializeFlowFile(file: FlowFile): string {
  const pretty = `${JSON.stringify(file, null, 2)}\n`;
  return byteLength(pretty) <= MAX_FLOW_FILE_BYTES ? pretty : `${JSON.stringify(file)}\n`;
}

function fileLevelIssues(raw: unknown): Issue[] {
  if (!isObject(raw)) return [{ path: '', code: CODE.invalidFormat }];
  const unknown = Object.keys(raw).filter((key) => !FILE_KEYS.has(key));
  if (unknown.length > 0) return unknown.map((key) => ({ path: key, code: CODE.unknownField }));
  if (raw.format !== FLOW_FILE_FORMAT) return [{ path: 'format', code: 'unknown_format' }];
  if (raw.version !== FLOW_FILE_VERSION) return [{ path: 'version', code: CODE.unsupportedVersion }];
  if (!Array.isArray(raw.flows)) return [{ path: 'flows', code: CODE.invalidFormat }];
  if (raw.flows.length === 0) return [{ path: 'flows', code: CODE.required }];
  return raw.flows.length > MAX_FLOWS_PER_FILE ? [{ path: 'flows', code: CODE.tooMany }] : [];
}

function parseJson(text: string): { value?: unknown; issues: Issue[] } {
  if (byteLength(text) > MAX_FLOW_FILE_BYTES) return { issues: [{ path: '', code: 'file_too_large' }] };
  try {
    return { value: JSON.parse(text.replace(/^﻿/, '')), issues: [] };
  } catch {
    return { issues: [{ path: '', code: CODE.invalidJson }] };
  }
}

// The shared decoder folds field case the way Go does, so {"URL": ...} would validate and then slip past
// every rule here that reads params.url. Definitions are all lowercase, so anything else is refused.
const unsafeKey = (key: string) => !SAFE_KEY.test(key) || FORBIDDEN_KEYS.has(key);

function childrenOf(value: unknown): unknown[] {
  if (Array.isArray(value)) return value;
  return isObject(value) ? Object.values(value) : [];
}

function keyIssue(value: unknown): Issue | null {
  if (isObject(value) && Object.keys(value).some(unsafeKey)) return { path: 'definition', code: CODE.unknownField };
  for (const child of childrenOf(value)) {
    const hit = keyIssue(child);
    if (hit) return hit;
  }
  return null;
}

function definitionIssues(definition: unknown): Issue[] {
  if (!isObject(definition)) return [{ path: 'definition', code: CODE.required }];
  // Size comes first: it bounds how deep the walks below can recurse on a hostile file.
  if (byteLength(JSON.stringify(definition) ?? '') > MAX_DEFINITION_BYTES) return [{ path: 'definition', code: CODE.tooLong }];
  const key = keyIssue(definition);
  if (key) return [key];
  return validateDefinition(definition).map((issue) => ({ ...issue, path: issue.path ? `definition.${issue.path}` : 'definition' }));
}

function entryShapeIssues(raw: Record<string, unknown>, seenRefs: Set<string>): Issue[] {
  const issues: Issue[] = Object.keys(raw)
    .filter((key) => !ENTRY_KEYS.has(key))
    .map((key) => ({ path: key, code: CODE.unknownField }));
  if (typeof raw.name !== 'string') issues.push({ path: 'name', code: CODE.required });
  if (typeof raw.ref !== 'string' || !REF_PATTERN.test(raw.ref)) issues.push({ path: 'ref', code: CODE.invalidFormat });
  else if (seenRefs.has(raw.ref)) issues.push({ path: 'ref', code: 'duplicate' });
  return issues;
}

function displayName(raw: unknown): string {
  const name = isObject(raw) ? str(raw.name) : '';
  return [...trimSpace(name).replace(/\p{Cc}/gu, ' ')].slice(0, MAX_NAME_LENGTH).join('');
}

function checkedEntry(raw: Record<string, unknown>, base: Omit<FlowImportEntry, 'status' | 'issues'>, seenRefs: Set<string>): FlowImportEntry {
  const shape = entryShapeIssues(raw, seenRefs);
  if (typeof raw.ref === 'string') seenRefs.add(raw.ref);
  const issues = [...shape, ...definitionIssues(raw.definition), ...(typeof raw.name === 'string' ? validateName(raw.name) : [])];
  if (issues.length > 0) return { ...base, status: 'invalid', issues };
  const { definition, setup } = neutralDefinition(raw.definition as Definition, (action) => action);
  const after = validateDefinition(definition).map((issue) => ({ ...issue, path: `definition.${issue.path}` }));
  if (after.length > 0) return { ...base, status: 'invalid', issues: after };
  return { ...base, status: 'ready', issues: [], setup, ref: raw.ref as string, definition };
}

function parseEntry(raw: unknown, index: number, seenRefs: Set<string>): FlowImportEntry {
  const base = { index, sourceName: displayName(raw), name: displayName(raw), renamed: false, setup: [] as Notes };
  if (!isObject(raw)) return { ...base, status: 'invalid', issues: [{ path: '', code: CODE.invalidFormat }] };
  try {
    return checkedEntry(raw, base, seenRefs);
  } catch {
    return { ...base, status: 'invalid', issues: [{ path: 'definition', code: CODE.invalidFormat }] };
  }
}

function suffixed(base: string, n: number): string {
  const tail = ` (${n})`;
  const room = MAX_NAME_LENGTH - [...tail].length;
  return `${trimSpace([...base].slice(0, room).join(''))}${tail}`;
}

function uniqueName(base: string, taken: Set<string>): string {
  let candidate = base;
  for (let n = 2; taken.has(candidate.toLowerCase()); n++) candidate = suffixed(base, n);
  taken.add(candidate.toLowerCase());
  return candidate;
}

function commandNames(definition: Definition): string[] {
  if (definition.trigger.type !== 'command') return [];
  const params = definition.trigger.params ?? {};
  return [params.name, ...list(params.aliases)].map((name) => str(name).replace(/^!/, '').toLowerCase());
}

interface Claims {
  slots: number;
  names: Set<string>;
  commands: Set<string>;
  alerts: Set<string>;
}

const emptied = (entry: FlowImportEntry, status: FlowImportStatus, issues: Issue[] = []): FlowImportEntry => ({
  ...entry,
  status,
  issues,
  setup: [],
  definition: undefined,
  ref: undefined
});

function landAutomation(entry: FlowImportEntry, claims: Claims): FlowImportEntry {
  const landing = { ...entry, landing: 'automation' as const };
  if (USES_TOKEN.test(JSON.stringify(entry.definition!.actions ?? []))) return emptied(landing, 'invalid', [{ path: 'definition.actions', code: 'uses_token' }]);
  if (claims.slots <= 0) return emptied(landing, 'over_limit');
  claims.slots -= 1;
  const name = uniqueName(entry.name, claims.names);
  const definition = entry.definition!;
  const shadowed = commandNames(definition).filter((command) => claims.commands.has(command));
  const setup = [...entry.setup, ...shadowed.map((detail): SetupNote => ({ code: 'command_in_use', path: 'trigger.params.name', detail }))];
  return { ...landing, name, renamed: name !== entry.sourceName, definition, setup };
}

// A command takes its name from the trigger and shadows any automation on it, so an existing one is a conflict, never a rename.
function landCommand(entry: FlowImportEntry, command: SimpleCommand, claims: Claims): FlowImportEntry {
  const names = [command.name, ...command.aliases].map((name) => name.toLowerCase());
  const landing = { ...entry, landing: 'command' as const, target: command.name, name: `!${command.name}` };
  if (names.some((name) => claims.commands.has(name))) return emptied(landing, 'conflict', [{ path: 'trigger.params.name', code: 'command_exists' }]);
  for (const name of names) claims.commands.add(name);
  return landing;
}

function landAlert(entry: FlowImportEntry, alert: AlertKey, claims: Claims): FlowImportEntry {
  const landing = { ...entry, landing: 'alert' as const, target: alert };
  if (claims.alerts.has(alert)) return emptied(landing, 'conflict', [{ path: 'trigger', code: 'alert_exists' }]);
  claims.alerts.add(alert);
  return landing;
}

function landEntry(entry: FlowImportEntry, claims: Claims): FlowImportEntry {
  if (entry.status !== 'ready') return entry;
  const layer = classifyFlow(entry.definition!);
  if (layer.layer === 'command') return landCommand(entry, layer.command, claims);
  if (layer.layer === 'alert') return landAlert(entry, layer.alert, claims);
  return landAutomation(entry, claims);
}

function claimsFrom(context: FlowImportContext): Claims {
  return {
    slots: Math.max(0, MAX_AUTOMATIONS_PER_CHANNEL - context.count),
    names: new Set(context.names.map((name) => name.toLowerCase())),
    commands: new Set(context.commands.map((name) => name.toLowerCase())),
    alerts: new Set(context.alerts)
  };
}

function deadRunCode(action: Action, landedByRef: Map<string, FlowImportEntry>): 'run_target_simple' | 'run_removed' | null {
  const target = landedByRef.get(str(action.params?.automation_id));
  if (target?.status === 'ready' && target.landing === 'automation') return null;
  return target?.status === 'ready' ? 'run_target_simple' : 'run_removed';
}

function dropDeadRuns(entry: FlowImportEntry, landedByRef: Map<string, FlowImportEntry>): FlowImportEntry {
  if (entry.status !== 'ready') return entry;
  const notes: Notes = [];
  const definition = mapDefinitionActions(entry.definition!, (action, path) => {
    const code = action.type === 'run' ? deadRunCode(action, landedByRef) : null;
    if (!code) return action;
    notes.push({ code, path });
    return null;
  });
  return notes.length === 0 ? entry : { ...entry, definition: compactDefinition(definition), setup: [...entry.setup, ...notes] };
}

function landedIndex(candidates: FlowImportEntry[], landed: FlowImportEntry[]): Map<string, FlowImportEntry> {
  return new Map(candidates.flatMap((entry, i): [string, FlowImportEntry][] => (entry.ref ? [[entry.ref, landed[i]!]] : [])));
}

function noteLinks(entry: FlowImportEntry, landedByRef: Map<string, FlowImportEntry>): FlowImportEntry {
  if (entry.status !== 'ready') return entry;
  const notes: Notes = [];
  mapDefinitionActions(entry.definition!, (action, path) => {
    if (action.type === 'run') notes.push({ code: 'run_linked', path, detail: landedByRef.get(str(action.params?.automation_id))?.name });
    return action;
  });
  return { ...entry, setup: [...entry.setup, ...notes] };
}

// Where a recipe lands decides whether a run can point at it, and dropping a run can make its owner simple, so this
// repeats until stable. Definitions only ever lose steps, so it ends.
function settleLandings(candidates: FlowImportEntry[], context: FlowImportContext): FlowImportEntry[] {
  let current = candidates;
  for (;;) {
    const claims = claimsFrom(context);
    const landed = current.map((entry) => landEntry(entry, claims));
    const byRef = landedIndex(current, landed);
    const next = current.map((entry) => dropDeadRuns(entry, byRef));
    if (next.every((entry, i) => entry === current[i])) return landed.map((entry) => noteLinks(entry, byRef));
    current = next;
  }
}

export function planFlowImport(text: string, context: FlowImportContext, selected?: readonly number[]): FlowImportPlan {
  const parsed = parseJson(text);
  if (parsed.issues.length > 0) return { ok: false, issues: parsed.issues };
  const fileIssues = fileLevelIssues(parsed.value);
  if (fileIssues.length > 0) return { ok: false, issues: fileIssues };
  const seenRefs = new Set<string>();
  const rawEntries = (parsed.value as { flows: unknown[] }).flows.map((raw, i) => parseEntry(raw, i, seenRefs));
  const wanted = (index: number) => selected === undefined || selected.includes(index);
  const candidates = rawEntries.map((entry) => (entry.status === 'ready' && !wanted(entry.index) ? emptied(entry, 'skipped') : entry));
  return { ok: true, entries: settleLandings(candidates, context), slots: Math.max(0, MAX_AUTOMATIONS_PER_CHANNEL - context.count) };
}

export function flowImportView(entry: FlowImportEntry): FlowImportEntryView {
  const { definition: _definition, ref: _ref, ...view } = entry;
  return { ...view, setup: entry.setup.filter(needsSetup), review: entry.setup.filter((note) => !needsSetup(note)) };
}

export function flowResultView(result: FlowImportResult): FlowImportResultView {
  return { ...result, setup: result.setup.filter(needsSetup), review: result.setup.filter((note) => !needsSetup(note)) };
}

// Holds each run's place: with the run simply gone, `command + chat + run` would classify as a plain command and land in the wrong layer.
function firstPassDefinition(definition: Definition): Definition {
  return mapDefinitionActions(definition, (action) => (action.type === 'run' ? { type: 'stop', enabled: false } : action));
}

function hasRuns(definition: Definition): boolean {
  let found = false;
  mapDefinitionActions(definition, (action) => {
    found ||= action.type === 'run';
    return action;
  });
  return found;
}

function linkedDefinition(definition: Definition, ids: Map<string, string>): Definition {
  return compactDefinition(
    mapDefinitionActions(definition, (action) => {
      if (action.type !== 'run') return action;
      const id = ids.get(str(action.params?.automation_id));
      return id ? { ...action, params: { automation_id: id } } : null;
    })
  );
}

const outcome = (entry: FlowImportEntry, status: FlowImportResultStatus, extra: Partial<FlowImportResult> = {}): FlowImportResult => ({
  index: entry.index,
  sourceName: entry.sourceName,
  name: entry.name,
  renamed: entry.renamed,
  status,
  landing: entry.landing,
  target: entry.target,
  issues: entry.issues,
  setup: entry.setup,
  ...extra
});

function previewOf(deps: FlowCommitDeps, definition: Definition, name: string): string | undefined {
  try {
    return deps.preview(definition, name);
  } catch {
    return undefined;
  }
}

async function safeWrite(deps: FlowCommitDeps, input: FlowWrite): Promise<FlowWriteResult> {
  return deps.write(input).catch((): FlowWriteResult => ({ ok: false, issues: [{ path: '', code: 'write_failed' }] }));
}

// A run dropped in the second pass can leave a simple flow, which saveFlow moves out of the automation table.
function finalLanding(definition: Definition): Pick<FlowImportResult, 'landing' | 'target'> {
  const layer = classifyFlow(definition);
  if (layer.layer === 'command') return { landing: 'command', target: layer.command.name };
  return layer.layer === 'alert' ? { landing: 'alert', target: layer.alert } : { landing: 'automation' };
}

interface Created {
  entry: FlowImportEntry;
  id: string;
  revision: number;
}

async function createEntry(entry: FlowImportEntry, deps: FlowCommitDeps): Promise<FlowWriteResult> {
  const definition = firstPassDefinition(entry.definition!);
  return safeWrite(deps, { name: entry.name, enabled: false, definition, preview: previewOf(deps, definition, entry.name) });
}

// Run targets must already exist on the modules write path and may form a cycle, so rows go in without their
// run steps first and every link lands in a second pass.
async function linkEntry(created: Created, ids: Map<string, string>, deps: FlowCommitDeps): Promise<FlowImportResult> {
  const { entry, id, revision } = created;
  const definition = linkedDefinition(entry.definition!, ids);
  const write: FlowWrite = { id, expectedRevision: revision, name: entry.name, enabled: false, definition, preview: previewOf(deps, definition, entry.name) };
  const linked = await safeWrite(deps, write);
  if (linked.ok) return outcome(entry, 'created', { id: linked.id, ...finalLanding(definition) });
  const setup = entry.setup.filter((note) => note.code !== 'run_linked');
  return outcome(entry, 'created', { id, setup: [...setup, { code: 'link_failed', path: 'actions' }] });
}

function droppedLinkNotes(entry: FlowImportEntry, ids: Map<string, string>): FlowImportEntry {
  const notes: Notes = [];
  mapDefinitionActions(entry.definition!, (action, path) => {
    if (action.type === 'run' && !ids.has(str(action.params?.automation_id))) notes.push({ code: 'run_removed', path });
    return action;
  });
  const dropped = new Set(notes.map((note) => note.path));
  const kept = entry.setup.filter((note) => note.code !== 'run_linked' || !dropped.has(note.path));
  return notes.length === 0 ? entry : { ...entry, setup: [...kept, ...notes] };
}

export async function commitFlowImport(entries: FlowImportEntry[], deps: FlowCommitDeps): Promise<FlowImportResult[]> {
  const results = new Map<number, FlowImportResult>();
  const created: Created[] = [];
  const ids = new Map<string, string>();
  for (const entry of entries) {
    if (entry.status !== 'ready') {
      results.set(entry.index, outcome(entry, entry.status as FlowImportResultStatus));
      continue;
    }
    const written = await createEntry(entry, deps);
    if (!written.ok) results.set(entry.index, outcome(entry, 'failed', { issues: written.issues }));
    else if (entry.landing === 'automation' && written.id) {
      ids.set(entry.ref!, written.id);
      created.push({ entry, id: written.id, revision: written.revision ?? 0 });
    } else results.set(entry.index, outcome(entry, 'created'));
  }
  for (const item of created) {
    const entry = droppedLinkNotes(item.entry, ids);
    const final = hasRuns(entry.definition!) ? await linkEntry({ ...item, entry }, ids, deps) : outcome(entry, 'created', { id: item.id });
    results.set(entry.index, final);
  }
  return entries.map((entry) => results.get(entry.index)!);
}
