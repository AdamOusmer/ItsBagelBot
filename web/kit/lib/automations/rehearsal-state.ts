// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expandSegments } from '../engine/rehearsal';
import { validateFetchDef } from '../engine/fetch-validate';
import { queryEscape, resolveComputedUtil } from '../engine/pure';
import type { VarToken } from '../engine/tmpl';
import { ACTION_TYPES } from './catalog';
import { comparePasses, needsSearchValue } from './compare';
import { SETUP_LOGIN } from './flow-setup';
import { MAX_COOLDOWN_SECONDS, MAX_MATH_EXPRESSION_RUNES, MAX_TEMPLATE_LENGTH } from './limits';
import { compilesAsRe2 } from './regex';
import { compareBytes, trimSpace } from './text';
import { extractJson } from './rehearsal-json';
import { CHAT_MODE_VALUES, type ChatModeValueRange } from './stream-types';
import type { Action, Condition, Params } from './types';
import type { ConditionOutcome, RehearsalContext, RehearsalScenario, RehearsalState } from './rehearsal-types';

type Text = string;
type Context = RehearsalContext;
type ExpandedParams = Record<Text, Text>;
type ExpandedLists = Record<Text, Text[]>;
type ActionOutcome = { status: 'ok' | 'failed' | 'skipped'; detail?: ExpandedParams };
type ScalarHandler = (params: Params, context: Context, expanded: ExpandedParams) => ActionOutcome;
type ActionHandler = (params: Params, context: Context, expanded: ExpandedParams, lists: ExpandedLists) => ActionOutcome;
type ConditionHandler = (params: Params, context: Context) => boolean | ConditionOutcome;

const text = (value: unknown): Text => String(value ?? '');
const number = (value: unknown): number => Number(value) || 0;
const stateName = (value: unknown): Text => trimSpace(text(value)).toLowerCase();
const nameOf = (params: Params): Text => text(params.name).toLowerCase();
const capped = (value: Text): Text => [...value].slice(0, MAX_TEMPLATE_LENGTH).join('');
const failed = (code: Text): ActionOutcome => ({ status: 'failed', detail: { code } });
const skipped = (code: Text): ActionOutcome => ({ status: 'skipped', detail: { code } });
const ok = (): ActionOutcome => ({ status: 'ok' });
const hasViewer = (context: Context): boolean => Number(context.scenario.userId) > 0;

function sanitized(value: Text | null): Text | null {
  return value === null ? null : value.replace(/[\x00-\x1f\x7f]/g, '').replace(/^[ /]+/, '');
}

export function messageTarget(args: Text, sender: Text): Text {
  const first = args.match(/[^\p{White_Space}]+/u)?.[0] ?? '';
  const target = args === '' ? sender : first.replace(/^@/, '');
  return sanitized(target)!.replace(/^@/, '');
}

export function createState(scenario: RehearsalScenario): RehearsalState {
  const seed = scenario.state;
  return {
    channelVars: Object.assign(Object.create(null), seed?.channelVars),
    viewerVars: Object.assign(Object.create(null), seed?.viewerVars),
    counters: Object.fromEntries(Object.entries(seed?.counters ?? {}).map(([name, value]) => [stateName(name), value])),
    points: Math.max(0, seed?.points ?? 0),
    cooldowns: {}, countdowns: { ...seed?.countdowns }, running: { poll: false, prediction: false }
  };
}

function variableStore(params: Params, context: Context): Record<string, string> {
  return params.scope === 'viewer' ? context.state.viewerVars : context.state.channelVars;
}

function counterValue(context: Context, name: Text): number | undefined {
  const counters = context.state.counters;
  return Object.hasOwn(counters, name) ? counters[name] : undefined;
}

function timerLeft(until: number | undefined, context: Context): Text | null {
  if (until === undefined) return null;
  return String(Math.max(0, Math.ceil((until - context.atMs) / 1000)));
}

function durationLeft(end: Text | undefined, context: Context): Text | null {
  if (end === undefined) return null;
  const seconds = Math.max(0, Math.ceil((Number(end) - context.atMs) / 1000));
  return context.formatDuration?.(seconds) ?? compactDuration(seconds);
}

export function compactDuration(value: number, units: readonly Text[] = ['d', 'h', 'm', 's']): Text {
  let seconds = value;
  const parts: Text[] = [];
  for (const [index, size] of [86400, 3600, 60, 1].entries()) {
    const count = Math.floor(seconds / size);
    if (count > 0) parts.push(`${count}${units[index]}`);
    seconds %= size;
    if (parts.length === 2) break;
  }
  return parts.join(' ');
}

const RANDOM_TOKENS = new Map<Text, (token: VarToken) => Text | null>([
  ['random', (token) => sanitized(randomSample(token))],
  ['choice', (token) => token.payload?.split(',')[0] ?? null]
]);

const TOKEN_READERS: Readonly<Record<string, (token: VarToken, context: Context) => Text | null>> = {
  local: (token, context) => context.locals[text(token.payload).toLowerCase()] ?? null,
  var: (token, context) => context.state.channelVars[text(token.payload).toLowerCase()] ?? null,
  viewervar: (token, context) => context.state.viewerVars[text(token.payload).toLowerCase()] ?? null,
  counter: (token, context) => counterValue(context, stateName(token.payload))?.toString() ?? null,
  points: (_, context) => String(context.state.points),
  cooldown: (token, context) => token.payload === 'left' ? durationLeft(context.locals['cooldown:end'], context) : null,
  countdown: (token, context) => token.payload === 'left' ? durationLeft(context.locals['countdown:end'], context) : null
};

function randomSample(token: VarToken): Text | null {
  if (token.payload === null) return '1';
  const bounds = token.payload.match(/^(\d+)-(\d+)(?::([^]))?$/u);
  if (!bounds) return null;
  if (BigInt(bounds[1]!) > BigInt(bounds[2]!)) return null;
  const value = String(BigInt(bounds[1]!));
  return bounds[3] === undefined ? value : value.replace(/\B(?=(\d{3})+(?!\d))/g, () => bounds[3]!);
}

function resolveValue(token: VarToken, context: Context): Text | null {
  const value = context.scenario.values?.[token.key];
  if (token.key === 'touser' || token.key === 'target') return value ?? '';
  const reader = TOKEN_READERS[token.name];
  if (reader) return sanitized(reader(token, context));
  if (value !== undefined) return sanitized(value);
  if (/^[1-9][0-9]*$/.test(token.name)) return positional(token, context);
  if (token.name === 'querystring') return queryEscape(context.scenario.args ?? '');
  return resolveComputedUtil(token);
}

function positional(token: VarToken, context: Context): Text {
  const words = context.scenario.args?.split(/\s+/).filter(Boolean) ?? [];
  const start = Number(token.name) - 1;
  if (token.payload === null) return sanitized(words[start] ?? '')!;
  const end = token.payload === '' ? words.length : Number(token.payload);
  return sanitized(words.slice(start, end).join(' '))!;
}

export function expandTemplate(template: Text, context: Context): { text: Text; missing: Text[]; random: boolean } {
  const missing: Text[] = [];
  let random = false;
  const segments = expandSegments(template, (token) => {
    const sample = RANDOM_TOKENS.get(token.name);
    const value = sample ? sample(token) : resolveValue(token, context);
    if (value === null || value === '') missing.push(token.key);
    if (sample && value !== null) random = true;
    return value ?? '';
  });
  return { text: segments.map((segment) => segment.text).join(''), missing: [...new Set(missing)], random };
}

const expand = (value: unknown, context: Context): Text => expandTemplate(text(value), context).text;
const ROLE_RANK: Readonly<Record<string, number>> = { viewer: 0, everyone: 0, sub: 1, vip: 2, mod: 3, lead_mod: 4, broadcaster: 5 };
const ORDER: Readonly<Record<string, (order: number) => boolean>> = {
  eq: (order) => order === 0, ne: (order) => order !== 0, gt: (order) => order > 0,
  gte: (order) => order >= 0, lt: (order) => order < 0, lte: (order) => order <= 0
};
const compareOrder = (op: unknown, order: number): boolean => ORDER[text(op)]?.(order) ?? false;
const compareNumber = (params: Params, value: number): boolean => compareOrder(params.op, Math.sign(value - number(params.value)));
const DECIMAL = /^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$/;

function trimDecimal(value: Text): Text {
  let start = 0;
  let end = value.length;
  while (start < end && ' \t\r\n'.includes(value[start]!)) start++;
  while (end > start && ' \t\r\n'.includes(value[end - 1]!)) end--;
  return value.slice(start, end);
}

function decimal(value: Text): number | null {
  const trimmed = trimDecimal(value);
  const parsed = DECIMAL.test(trimmed) ? Number(trimmed) : NaN;
  return Number.isFinite(parsed) ? parsed : null;
}

function variableComparison(params: Params, context: Context): boolean {
  if (params.scope === 'viewer' && !hasViewer(context)) return false;
  const current = variableStore(params, context)[nameOf(params)];
  if (params.op === 'exists') return current !== undefined;
  if (params.op === 'not_exists') return current === undefined;
  return variableValueComparison(text(params.op), current ?? '', expand(params.value, context));
}

function variableValueComparison(op: Text, current: Text, right: Text): boolean {
  if (needsSearchValue(op)) return comparePasses(op, current, right);
  const leftNumber = decimal(current);
  const rightNumber = decimal(right);
  const numeric = leftNumber !== null && rightNumber !== null;
  const order = numeric ? Math.sign(leftNumber! - rightNumber!) : compareBytes(current ?? '', right);
  return compareOrder(op, order);
}

type UserList = { logins: Text[]; ids: Text[] };
type ListMode = 'allow' | 'deny';

function userList(params: Params, mode: ListMode): UserList {
  return { logins: (params[mode] ?? []) as Text[], ids: (params[`${mode}_ids`] ?? []) as Text[] };
}

function containsFold(list: Text[], value: Text): boolean {
  const folded = value.toLowerCase();
  return list.some((entry) => entry.toLowerCase() === folded);
}

function userID(value: Text): bigint | null {
  if (!/^\d+$/.test(value)) return null;
  const id = BigInt(value);
  return id > 0n && id <= 0xffffffffffffffffn ? id : null;
}

function userListed({ logins, ids }: UserList, scenario: RehearsalScenario): boolean {
  if (containsFold(logins, scenario.user ?? '')) return true;
  const id = userID(scenario.userId ?? '');
  return id !== null && ids.some((entry) => userID(entry) === id);
}

function userComparison(params: Params, context: Context): boolean | ConditionOutcome {
  const deny = userList(params, 'deny');
  const allow = userList(params, 'allow');
  if (containsFold([...deny.logins, ...allow.logins], SETUP_LOGIN)) return { passes: false, code: 'setup_required' };
  if (userListed(deny, context.scenario)) return false;
  return (allow.logins.length === 0 && allow.ids.length === 0) || userListed(allow, context.scenario);
}

function categoryComparison(params: Params, context: Context): boolean {
  const game = context.scenario.game;
  if (game === undefined) return context.overrides[context.path] !== 'no';
  const { logins: allow } = userList(params, 'allow');
  const { logins: deny } = userList(params, 'deny');
  if (containsFold(deny, game)) return false;
  return allow.length === 0 || containsFold(allow, game);
}

function argRegexComparison(params: Params, context: Context): boolean {
  const pattern = text(params.pattern);
  if (!compilesAsRe2(pattern)) return false;
  const leading = /^\(\?([ims]+)\)/.exec(pattern);
  const flags = leading ? [...new Set(leading[1])].join('') : '';
  const source = pattern.slice(leading?.[0].length ?? 0).replace(/\(\?P</g, '(?<');
  try {
    return new RegExp(source, flags).test(context.scenario.args ?? '');
  } catch {
    return false;
  }
}

const namedCooldownKey = (name: Text): Text => `named:${name}`;

function cooldownKeys(params: Params, context: Context): Text[] {
  if (params.name) return [namedCooldownKey(nameOf(params))];
  const ordinal = context.cooldownOrdinals.get(context.path) ?? 0;
  const suffix = ordinal > 0 ? `:${ordinal}` : '';
  const keys: Text[] = [];
  if (number(params.global_s) > 0) keys.push(`global${suffix}`);
  if (number(params.user_s) > 0) keys.push(`viewer:${userID(context.scenario.userId ?? '') ?? 0n}${suffix}`);
  return keys;
}

function cooldownComparison(params: Params, context: Context): boolean {
  const keys = cooldownKeys(params, context);
  if (context.overrides[context.path] === 'cooling') {
    const seconds = Math.max(number(params.global_s), number(params.user_s));
    context.locals['cooldown:end'] = String(context.atMs + seconds * 1000);
    return false;
  }
  const windows = keys.map((key) => cooldownWindow(key, context));
  if (windows.some((until) => until > context.atMs)) {
    noteCooldown(windows, context);
    return false;
  }
  for (const key of keys) armCooldown(key, params, context);
  return true;
}

function cooldownWindow(key: Text, context: Context): number {
  const seeded = context.overrides[context.path] === 'ready' ? 0 : cooldownSeed(key, context.scenario.state?.cooldowns);
  return context.state.cooldowns[key] ?? seeded;
}

function cooldownSeed(key: Text, seeds: RehearsalState['cooldowns'] = {}): number {
  if (Object.hasOwn(seeds, key)) return seeds[key]!;
  if (!key.startsWith('named:')) return 0;
  const name = key.slice('named:'.length);
  return name !== 'global' && Object.hasOwn(seeds, name) ? seeds[name]! : 0;
}

function noteCooldown(windows: number[], context: Context): void {
  const until = Math.max(0, ...windows);
  if (until > context.atMs) context.locals['cooldown:end'] = String(until);
}

function armCooldown(key: Text, params: Params, context: Context): void {
  const seconds = key.startsWith(VIEWER_COOLDOWN_PREFIX) ? number(params.user_s) : number(params.global_s);
  context.state.cooldowns[key] = context.atMs + seconds * 1000;
}

function chanceComparison(params: Params, context: Context): boolean {
  const percent = number(params.percent);
  if (percent >= 100) return true;
  return percent > 0 && context.overrides[context.path] !== 'no';
}

function viewerComparison(_params: Params, context: Context): boolean {
  return hasViewer(context) && context.overrides[context.path] !== 'no';
}

const CONDITIONS: Readonly<Record<string, ConditionHandler>> = {
  perm: (params, context) => ROLE_RANK[context.scenario.role ?? 'viewer']! >= (ROLE_RANK[text(params.level)] ?? 0),
  users: userComparison,
  compare: (params, context) => comparePasses(text(params.op), expand(params.left, context), expand(params.right, context), params.ignore_case === true),
  var: variableComparison,
  chance: chanceComparison,
  cooldown: cooldownComparison,
  live: (params, context) => context.scenario.live === (params.state === 'live'),
  category: categoryComparison,
  arg_regex: argRegexComparison,
  follow: viewerComparison,
  account_age: viewerComparison,
  points: (params, context) => hasViewer(context) && compareNumber(params, context.state.points),
  counter: (params, context) => compareNumber(params, counterValue(context, stateName(params.name)) ?? 0)
};

export function evaluateCondition(condition: Condition, context: Context): ConditionOutcome {
  const outcome = CONDITIONS[condition.type]?.(condition.params ?? {}, context) ?? false;
  return typeof outcome === 'boolean' ? { passes: outcome } : outcome;
}

function saveLocal(params: Params, value: Text, context: Context): void {
  const name = text(params.save_as).toLowerCase();
  if (/^[a-z0-9]{1,32}$/.test(name)) context.locals[name] = capped(value);
}

function addVariable(current: Text, amount: number): Text | null {
  const trimmed = trimSpace(current);
  if (/^[+-]?\d+$/.test(trimmed) && Number.isInteger(amount)) return addInteger(trimmed, amount);
  const sum = (decimal(trimmed) ?? 0) + amount;
  if (!Number.isFinite(sum)) return null;
  return fixedNumber(sum);
}

function addInteger(current: Text, amount: number): Text | null {
  const value = integerValue(current);
  if (value === null) return null;
  return integerValue(String(value + BigInt(amount)))?.toString() ?? null;
}

function integerValue(value: Text): bigint | null {
  if (!/^[+-]?\d+$/.test(value)) return null;
  const parsed = BigInt(value);
  return parsed < -(2n ** 63n) || parsed > 2n ** 63n - 1n ? null : parsed;
}

function writeVariable(params: Params, context: Context, mode: Text, expanded: ExpandedParams): ActionOutcome {
  if (params.scope === 'viewer' && !hasViewer(context)) return failed('no_viewer');
  const store = variableStore(params, context);
  const name = nameOf(params);
  if (mode === 'delete') {
    delete store[name];
    return ok();
  }
  const value = mode === 'set' ? text(expanded.value) : addVariable(store[name] ?? '', number(params.amount));
  if (value === null) return failed('var_limit');
  if ([...value].length > MAX_TEMPLATE_LENGTH) return failed('var_limit');
  store[name] = value;
  return { status: 'ok', detail: { value } };
}

function addVariableStep(params: Params, context: Context, expanded: ExpandedParams): ActionOutcome {
  const outcome = writeVariable(params, context, 'add', expanded);
  return { ...outcome, detail: { value: expanded.amount ?? '', ...outcome.detail } };
}

type Operands = { input: Text; arg1: Text; arg2: Text };
function simpleCase(input: Text, mode: 'upper' | 'lower'): Text {
  return [...input].map((rune) => caseRune(rune, mode)).join('');
}

function caseRune(rune: Text, mode: 'upper' | 'lower'): Text {
  if (mode === 'lower' && rune === 'İ') return 'i';
  const changed = mode === 'upper' ? rune.toUpperCase() : rune.toLowerCase();
  return [...changed].length === 1 ? changed : rune;
}

function splitParts({ input, arg1 }: Operands): Text[] {
  return arg1 === '' ? [...input] : input.split(arg1);
}

const TEXT_OPS: Readonly<Record<string, (operands: Operands) => Text>> = {
  upper: ({ input }) => simpleCase(input, 'upper'), lower: ({ input }) => simpleCase(input, 'lower'),
  trim: ({ input }) => trimSpace(input),
  replace: ({ input, arg1, arg2 }) => arg1 === '' ? input : input.split(arg1).join(arg2),
  contains: ({ input, arg1 }) => String(comparePasses('contains', input, arg1)),
  url_encode: ({ input }) => queryEscape(input),
  count: (operands) => String(splitParts(operands).length),
};

function optionalInteger(value: Text, fallback: number): number | null {
  const trimmed = trimSpace(value);
  if (trimmed === '') return fallback;
  const parsed = integerValue(trimmed);
  return parsed === null ? null : Number(parsed);
}

function substring({ input, arg1, arg2 }: Operands): Text | null {
  const start = optionalInteger(arg1, 0);
  const length = optionalInteger(arg2, 2 * MAX_TEMPLATE_LENGTH);
  if (start === null || length === null) return null;
  const runes = [...input];
  const from = Math.min(Math.max(start, 0), runes.length);
  return runes.slice(from, from + Math.max(length, 0)).join('');
}

function splitPick(operands: Operands): Text | null {
  const parts = splitParts(operands);
  if (trimSpace(operands.arg2).toLowerCase() === 'random') return parts[0] ?? '';
  const position = optionalInteger(operands.arg2, NaN);
  if (position === null || Number.isNaN(position)) return null;
  const index = position < 0 ? parts.length + position : position - 1;
  return parts[index] ?? '';
}

const COMPLEX_TEXT_OPS: Readonly<Record<Text, (operands: Operands) => Text | null>> = { substring, split_pick: splitPick };
const capOperand = (value: Text = '') => [...value].slice(0, 2 * MAX_TEMPLATE_LENGTH).join('');

function textResult(op: Text, expanded: ExpandedParams): Text | null | undefined {
  if (op === 'length') return String([...(expanded.input ?? '')].length);
  const handler = TEXT_OPS[op] ?? COMPLEX_TEXT_OPS[op];
  return handler?.({ input: capOperand(expanded.input), arg1: capOperand(expanded.arg1), arg2: capOperand(expanded.arg2) });
}

function textStep(params: Params, context: Context, expanded: ExpandedParams): ActionOutcome {
  const result = textResult(text(params.op), expanded);
  if (result == null) return failed('text_error');
  saveLocal(params, result, context);
  const random = params.op === 'split_pick' && trimSpace(capOperand(expanded.arg2)).toLowerCase() === 'random';
  return { status: 'ok', detail: { value: capped(result), ...(random ? { random: '1' } : {}) } };
}

type MathFunction = { min: number; max: number; apply: (values: number[]) => number };
const unary = (apply: (value: number) => number): MathFunction => ({ min: 1, max: 1, apply: (values) => apply(values[0]!) });
const MATH_FUNCTIONS: Readonly<Record<string, MathFunction>> = {
  round: unary((value) => Math.sign(value) * Math.floor(Math.abs(value) + 0.5)),
  floor: unary(Math.floor), ceil: unary(Math.ceil), abs: unary(Math.abs),
  min: { min: 1, max: 200, apply: (values) => Math.min(...values) },
  max: { min: 1, max: 200, apply: (values) => Math.max(...values) },
  random: { min: 2, max: 2, apply: (values) => Math.min(...values) }
};
const SUM: Readonly<Record<string, (left: number, right: number) => number>> = { '+': (a, b) => a + b, '-': (a, b) => a - b };
const PRODUCT: Readonly<Record<string, (left: number, right: number) => number>> = {
  '*': (a, b) => a * b, '/': (a, b) => b === 0 ? NaN : a / b, '%': (a, b) => b === 0 ? NaN : a % b
};

class MathParser {
  position = 0;
  failed = false;
  random = false;
  constructor(readonly source: Text) {}

  fail(): number {
    this.failed = true;
    this.position = this.source.length;
    return 0;
  }

  peek(): Text {
    while (/[ \t]/.test(this.source[this.position] ?? '\0')) this.position++;
    return this.source[this.position] ?? '';
  }

  take(want: Text): boolean {
    if (this.peek() !== want) return false;
    this.position++;
    return true;
  }

  binary(operators: typeof SUM, operand: () => number): number {
    let left = operand();
    for (let apply = operators[this.peek()]; apply; apply = operators[this.peek()]) {
      const operator = this.peek();
      this.position++;
      const right = operand();
      const zeroDivision = (operator === '/' || operator === '%') && right === 0;
      left = zeroDivision ? this.fail() : apply(left, right);
    }
    return left;
  }

  sum(): number { return this.binary(SUM, () => this.product()); }
  product(): number { return this.binary(PRODUCT, () => this.unary()); }

  unary(): number {
    if (this.take('-')) return -this.unary();
    const base = this.primary();
    return this.take('^') ? base ** this.unary() : base;
  }

  primary(): number {
    if (this.take('(')) return this.group();
    const next = this.peek();
    if (/[a-z]/.test(next) && next !== '') return this.call();
    const match = /^[0-9.]+/.exec(this.source.slice(this.position));
    if (!match) return this.fail();
    this.position += match[0].length;
    const value = /^(?:[0-9]+\.?[0-9]*|\.[0-9]+)$/.test(match[0]) ? Number(match[0]) : NaN;
    return Number.isFinite(value) ? value : this.fail();
  }

  group(): number {
    const value = this.sum();
    return this.take(')') ? value : this.fail();
  }

  call(): number {
    const name = /^[a-z]+/.exec(this.source.slice(this.position))![0];
    this.position += name.length;
    const fn = MATH_FUNCTIONS[name];
    if (!fn || !this.take('(')) return this.fail();
    const args = [this.sum()];
    while (this.take(',')) args.push(this.sum());
    const valid = this.take(')') && args.length >= fn.min && args.length <= fn.max;
    if (!valid) return this.fail();
    if (name === 'random') this.random = true;
    return fn.apply(args);
  }
}

function fixedNumber(value: number): Text {
  const raw = String(value);
  if (!/[eE]/.test(raw)) return raw;
  const [coefficient, power] = raw.split('e');
  const sign = coefficient!.startsWith('-') ? '-' : '';
  const digits = coefficient!.replace('-', '').replace('.', '');
  const point = coefficient!.replace('-', '').split('.')[0]!.length + Number(power);
  if (point <= 0) return `${sign}0.${'0'.repeat(-point)}${digits}`;
  return sign + digits.padEnd(point, '0').slice(0, point) + (point < digits.length ? `.${digits.slice(point)}` : '');
}

function mathStep(params: Params, context: Context): ActionOutcome {
  const expression = expand(params.expression, context);
  if ([...expression].length > MAX_MATH_EXPRESSION_RUNES) return failed('math_error');
  const parser = new MathParser(expression);
  const value = parser.sum();
  const invalid = parser.failed || !Number.isFinite(value) || parser.peek() !== '';
  if (invalid) return failed('math_error');
  saveLocal(params, fixedNumber(value), context);
  return { status: 'ok', detail: { value: fixedNumber(value), ...(parser.random ? { random: '1' } : {}) } };
}

function failHttp(context: Context, code: Text): ActionOutcome {
  context.locals.error = code;
  return failed(code);
}

function httpStep(params: Params, context: Context, expanded: ExpandedParams): ActionOutcome {
  const errors = validateFetchDef({ name: 'a', url: expanded.url ?? '', kind: 'plain', path: [], keyLabel: '' });
  if (errors.url) return failHttp(context, 'bad_def');
  if (context.overrides[context.path] === 'failure') return failHttp(context, context.overrides[`${context.path}.error`] ?? 'upstream_error');
  const response = context.overrides[`${context.path}.response`] ?? '';
  try {
    const value = extractJson(response, (params.json_path ?? []) as Text[]);
    delete context.locals.error;
    saveLocal(params, value, context);
    return { status: 'ok', detail: { response: value } };
  } catch {
    return failHttp(context, 'bad_def');
  }
}

function timerAdded(params: Params, context: Context): number {
  const per = number(params.per);
  const amount = number(context.scenario.values?.['event.amount']);
  const multiples = per > 0 ? Math.trunc(amount / per) : 1;
  return Math.min(MAX_COOLDOWN_SECONDS, number(params.seconds) * multiples) * 1000;
}

function timerSlot(name: Text, context: Context, mode: Text) {
  if (mode === 'cooldown_add') {
    const key = namedCooldownKey(name);
    return { key, store: context.state.cooldowns, until: cooldownWindow(key, context), token: 'cooldown:end' };
  }
  const store = context.state.countdowns;
  return { key: name, store, until: store[name] ?? 0, token: 'countdown:end' };
}

function timerStep(params: Params, context: Context, mode: Text): ActionOutcome {
  const added = timerAdded(params, context);
  if (added <= 0) return { status: 'skipped', detail: { code: 'below_rate' } };
  const name = nameOf(params);
  const { key, store, until, token } = timerSlot(name, context, mode);
  const running = until > context.atMs;
  if (mode !== 'countdown_start' && !running) return { status: 'skipped', detail: { code: 'not_running' } };
  const base = mode === 'countdown_start' ? context.atMs : Math.max(context.atMs, until);
  store[key] = Math.min(context.atMs + MAX_COOLDOWN_SECONDS * 1000, base + added);
  context.locals[token] = String(store[key]);
  return { status: 'ok', detail: { name, left: timerLeft(store[key], context)! } };
}

function countdownStopStep(params: Params, context: Context): ActionOutcome {
  const name = nameOf(params);
  const countdowns = context.state.countdowns;
  const until = countdowns[name];
  if (!(until > context.atMs)) return failed('not_running');
  delete countdowns[name];
  if (context.locals['countdown:end'] === text(until)) delete context.locals['countdown:end'];
  return { status: 'ok', detail: { value: name } };
}

function cooldownResetStep(params: Params, context: Context): ActionOutcome {
  const name = nameOf(params);
  context.state.cooldowns[namedCooldownKey(name)] = 0;
  return { status: 'ok', detail: { value: name } };
}

function replyStep(params: Params, context: Context, expanded: ExpandedParams): ActionOutcome {
  const message = expanded.text ?? '';
  if (params.thread === true && context.hasChatMessage) return ok();
  const viewer = context.scenario.user?.replace(/^@/, '') ?? '';
  const prefix = viewer && trimSpace(message) !== '' ? `@${viewer} ` : '';
  return { status: 'ok', detail: { text: prefix + message } };
}

function requiredParams(fields: readonly Text[], code: Text, next: ScalarHandler = ok): ScalarHandler {
  return (params, context, expanded) => fields.some((field) => !trimSpace(expanded[field] ?? ''))
    ? failed(code) : next(params, context, expanded);
}

function liveGated(canRunOffline: (params: Params) => boolean = () => false): ScalarHandler {
  return (params, context) => context.scenario.live || canRunOffline(params)
    ? ok() : { status: 'skipped', detail: { code: 'offline' } };
}

const lightsLive = liveGated((params) => params.any_time === true);

function lightsStep(params: Params, context: Context, expanded: ExpandedParams): ActionOutcome {
  const outcome = lightsLive(params, context, expanded);
  const refs: unknown[] = Array.isArray(params.extra_lights) ? params.extra_lights : [];
  const extras = refs.filter((ref): ref is Text => typeof ref === 'string' && ref.includes('/'));
  const devices = extras.map((ref) => trimSpace(ref.slice(0, ref.indexOf('/'))));
  const count = new Set([trimSpace(expanded.device!), ...devices]).size;
  return { ...outcome, detail: { ...outcome.detail, count: String(count) } };
}

const resolvedTarget = (login: Text): ActionOutcome => ({ status: 'ok', detail: { login } });
const loginText = (value: Text = ''): Text => trimSpace(trimSpace(value).replace(/^@/, ''));

function scenarioViewer(context: Context): ActionOutcome {
  if (!hasViewer(context)) return failed('no_viewer');
  return resolvedTarget(loginText(context.scenario.user) || context.scenario.userId!);
}

function mentionedTarget(scenario: RehearsalScenario): ActionOutcome {
  const mention = scenario.message?.match(/(?<![\w@])@(\w+)/)?.[1];
  return mention ? resolvedTarget(mention) : failed('no_target');
}

function pickedTarget(params: Params): ActionOutcome {
  const id = text(params.user_id);
  return /^0*[1-9]\d*$/.test(id) ? resolvedTarget(id) : failed('no_target');
}

function targetStep(): ScalarHandler {
  const loginStep = requiredParams(['login'], 'empty_login', (_params, _context, expanded) => resolvedTarget(expanded.login!));
  return (params, context, expanded) => {
    switch (params.who) {
      case 'viewer': return scenarioViewer(context);
      case 'mention': return mentionedTarget(context.scenario);
      case 'user': return pickedTarget(params);
      default: return loginStep(params, context, { ...expanded, login: loginText(expanded.login) });
    }
  };
}

const resolveWhisperTarget = targetStep();

function whisperStep(params: Params, context: Context, expanded: ExpandedParams): ActionOutcome {
  const target = resolveWhisperTarget(params, context, expanded);
  if (target.status !== 'ok') return target;
  return trimSpace(expanded.text ?? '') === '' ? failed('empty') : target;
}

type Contest = keyof RehearsalState['running'];

function startContest(type: Contest, field: 'choices' | 'outcomes'): ActionHandler {
  return (_params, context, _expanded, lists) => {
    const items = (lists[field] ?? []).map(trimSpace).filter(Boolean);
    const detail = { [field]: items.join('\n') };
    if (items.length < 2) return { status: 'failed', detail: { ...detail, code: 'too_few_choices' } };
    context.state.running[type] = true;
    return { status: 'ok', detail };
  };
}

function endContest(type: Contest): ActionHandler {
  return (params, context) => {
    if (!context.state.running[type]) return skipped('nothing_running');
    context.state.running[type] = params.status === 'lock';
    return ok();
  };
}

const clamp = (value: number, [min, max]: ChatModeValueRange): number => Math.max(min, Math.min(max, value));

const EFFECTIVE_CHAT_MODE: ReadonlyMap<Text, (value: number) => number> = new Map([
  ['slow', (value) => clamp(value || 30, CHAT_MODE_VALUES.slow[1])],
  ['followers', (value) => clamp(value, CHAT_MODE_VALUES.followers[0])],
  ['chat_delay', (value) => CHAT_MODE_VALUES.chat_delay.find(([seconds]) => seconds === value)?.[0] ?? CHAT_MODE_VALUES.chat_delay[0][0]]
]);

function chatModeStep(params: Params): ActionOutcome {
  const value = params.on === true ? EFFECTIVE_CHAT_MODE.get(text(params.mode))?.(number(params.value)) : 0;
  return { status: 'ok', detail: { value: String(value ?? 0) } };
}

const targetedAction = targetStep();
const MAX_SET_POINTS = ACTION_TYPES.find((action) => action.type === 'points_set')!.fields.find((field) => field.name === 'amount')!.max!;

function pointsMode(params: Params, expanded: ExpandedParams): unknown {
  const mode = params.who || 'login';
  return mode === 'login' && !loginText(expanded.login) ? 'viewer' : mode;
}

function targetsViewer(mode: Params['who'], target: Text, context: Context): boolean {
  if (mode === 'viewer') return true;
  if (!hasViewer(context)) return false;
  if (mode === 'user') return target.replace(/^0+/, '') === context.scenario.userId?.replace(/^0+/, '');
  return target.toLowerCase() === loginText(context.scenario.user).toLowerCase();
}

function pointsStep(set: boolean): ActionHandler {
  return (params, context, expanded) => {
    const who = pointsMode(params, expanded);
    if (who === 'viewer' && !loginText(context.scenario.user)) return failed('no_viewer');
    const outcome = targetedAction({ ...params, who }, context, expanded);
    if (outcome.status !== 'ok') return outcome;
    const target = outcome.detail!.login!;
    if (!targetsViewer(who, target, context)) return { ...outcome, detail: { ...outcome.detail, target } };
    const amount = number(params.amount);
    context.state.points = Math.max(0, set ? Math.min(MAX_SET_POINTS, amount) : context.state.points + amount);
    return { ...outcome, detail: { ...outcome.detail, target, value: text(context.state.points) } };
  };
}

function pointsSpendStep(params: Params, context: Context): ActionOutcome {
  if (!hasViewer(context) || !loginText(context.scenario.user)) return failed('no_viewer');
  const amount = number(params.amount);
  const balance = context.state.points;
  if (balance < amount) return { status: 'failed', detail: { code: 'insufficient_points', value: text(balance) } };
  context.state.points -= amount;
  return { status: 'ok', detail: { value: text(context.state.points) } };
}

function counterStep(set: boolean): ActionHandler {
  return (params, context) => {
    const name = stateName(params.name);
    const value = set ? number(params.value) : (counterValue(context, name) ?? 0) + number(params.amount);
    context.state.counters[name] = value;
    return { status: 'ok', detail: { name, value: text(value) } };
  };
}

function redemptionStep(params: Params, context: Context): ActionOutcome {
  if (context.triggerType !== 'reward') return failed('no_redemption');
  const value = text(params.status);
  return ['fulfill', 'cancel'].includes(value) ? { status: 'ok', detail: { value } } : failed('no_redemption');
}

const REWARD_CHANGE_TYPES = { enabled: 'boolean', cost: 'number', paused: 'boolean', cooldown_s: 'number' } as const;

function rewardUpdateStep(params: Params): ActionOutcome {
  const changes = Object.entries(REWARD_CHANGE_TYPES).flatMap(([key, type]) =>
    typeof params[key] === type ? [[key, text(params[key])] as const] : []);
  if (!changes.length) return failed('nothing_to_change');
  return { status: 'ok', detail: { ...Object.fromEntries(changes), reset_on_stream_end: text(params.reset_on_stream_end === true) } };
}

const ACTIONS: Readonly<Record<string, ActionHandler>> = {
  reply: replyStep,
  shoutout: targetStep(), timeout: targetStep(),
  ban: targetStep(), unban: targetStep(), mod: targetStep(), vip: targetStep(),
  whisper: whisperStep,
  poll: startContest('poll', 'choices'),
  prediction: startContest('prediction', 'outcomes'),
  poll_end: endContest('poll'),
  prediction_end: endContest('prediction'),
  chat_mode: chatModeStep,
  delete_message: (_params, context) => context.hasChatMessage ? ok() : failed('no_message'),
  var_set: (params, context, expanded) => writeVariable(params, context, 'set', expanded),
  var_add: addVariableStep,
  var_delete: (params, context, expanded) => writeVariable(params, context, 'delete', expanded),
  text: textStep, math: mathStep, http_get: httpStep, http_post: httpStep,
  counter_add: counterStep(false),
  points_add: pointsStep(false),
  countdown_start: (params, context) => timerStep(params, context, 'countdown_start'),
  countdown_add: (params, context) => timerStep(params, context, 'countdown_add'),
  cooldown_add: (params, context) => timerStep(params, context, 'cooldown_add'),
  lights: requiredParams(['device', 'sku'], 'no_device', lightsStep),
  raid: targetStep(),
  commercial: liveGated(),
  marker: liveGated(),
  clip: liveGated(),
  ad_snooze: liveGated(),
  run: requiredParams(['automation_id'], 'not_found', (_params, context) =>
    context.overrides[context.path] === 'failure' ? failed('child_failed') : ok()),
  counter_set: counterStep(true),
  points_set: pointsStep(true),
  redemption: redemptionStep,
  reward_update: rewardUpdateStep,
  points_spend: pointsSpendStep,
  countdown_stop: countdownStopStep,
  cooldown_reset: cooldownResetStep
};

function expandParam(value: unknown, context: Context): ReturnType<typeof expandTemplate> & { items?: Text[] } {
  if (!Array.isArray(value)) return expandTemplate(text(value), context);
  const items = value.map((item) => expandTemplate(text(item), context));
  const rendered = items.map((item) => item.text);
  return { text: rendered.join('\n'), missing: items.flatMap((item) => item.missing), items: rendered, random: items.some((item) => item.random) };
}

function expandActionParam(action: Action, key: Text, context: Context): ReturnType<typeof expandParam> {
  const value = action.params?.[key];
  if (action.type !== 'clip' || key !== 'reply') return expandParam(value, context);
  const values = { ...context.scenario.values, 'clip:url': '{clip:url}', 'clip:title': '{clip:title}' };
  return expandParam(value, { ...context, scenario: { ...context.scenario, values } });
}

export function applyAction(action: Action, context: Context): ActionOutcome & { detail: ExpandedParams } {
  const params = action.params ?? {};
  const values = Object.keys(params).map((key) => [key, expandActionParam(action, key, context)] as const);
  const expanded = Object.fromEntries(values.map(([key, value]) => [key, value.text]));
  const lists = Object.fromEntries(values.flatMap(([key, value]) => value.items ? [[key, value.items]] : []));
  const missing = [...new Set(values.flatMap(([, value]) => value.missing))].join(',');
  const sample: ExpandedParams = values.some(([, value]) => value.random) ? { random: '1' } : {};
  const outcome = ACTIONS[action.type]?.(params, context, expanded, lists) ?? ok();
  return { ...outcome, detail: { type: action.type, ...expanded, missing, ...sample, ...outcome.detail } };
}
