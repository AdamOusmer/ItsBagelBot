// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type {
  CollisionRef,
  ImportDiagnostic,
  ImportManifest,
  ImportStats,
  ManifestCommand,
  ManifestQuote,
  ManifestTimer,
  ManifestTrigger,
  Perm
} from './types';
import { IMPORT_ITEM_CAPS } from './types';
import { slugifyName } from '../engine/fetch-tokens';
import { FETCH_NAME_MAX } from '../engine/fetch-validate';
import { lex, type VarToken } from '../engine/tmpl';
export { intactSpan } from '../engine/tmpl';

export function mappedSpans(text: string): VarToken[] {
  return lex(text).filter((t): t is VarToken => t.kind === 'var');
}

export const CODE = {
  manifestEmpty: 'manifest_empty',
  unsupportedSource: 'unsupported_source',
  credentialRequired: 'credential_required',
  fileRequired: 'file_required',
  fileDecodeFailed: 'file_decode_failed',
  fileTooLarge: 'file_too_large',
  fetchFailed: 'fetch_failed',
  parseFailed: 'parse_failed',
  collisionLookupFailed: 'collision_lookup_failed',
  nameInvalid: 'command_name_invalid',
  aliasInvalid: 'command_alias_invalid',
  responseInvalid: 'command_response_invalid',
  responseTruncated: 'command_response_truncated',
  responseLineDropped: 'command_response_line_dropped',
  permissionUnmapped: 'command_permission_unmapped',
  cooldownClamped: 'command_cooldown_clamped',
  variableUnmapped: 'command_variable_unmapped',
  intervalClamped: 'timer_interval_clamped',
  timerMessageEmpty: 'timer_message_empty',
  triggerInvalid: 'trigger_invalid',
  quoteTextInvalid: 'quote_text_invalid',
  quoteDateInvalid: 'quote_date_invalid',
  automodTermsTooMany: 'automod_terms_too_many',
  moduleReadFailed: 'module_read_failed',
  writeFailed: 'write_failed',
  commandTooMany: 'command_too_many',
  timerTooMany: 'timer_too_many',
  triggerTooMany: 'trigger_too_many',
  quoteTooMany: 'quote_too_many'
} as const;

export const MAX_RESPONSE_LINE_BYTES = 500;
export const MAX_RESPONSE_LINES = 5;
export const MAX_COOLDOWN_SECONDS = 86400;

const encoder = new TextEncoder();
function byteLen(s: string): number {
  return encoder.encode(s).length;
}

export function normalizeName(name: string): string {
  return name.trim().replace(/^!/, '').trim().toLowerCase();
}

export function clampCooldown(seconds: number): number {
  if (seconds <= 0) return 0;
  if (seconds > MAX_COOLDOWN_SECONDS) return MAX_COOLDOWN_SECONDS;
  return seconds;
}

function truncateBytes(s: string, limit: number): string {
  const bytes = encoder.encode(s);
  if (bytes.length <= limit) return s;
  let cut = limit;
  while (cut > 0 && (bytes[cut] & 0xc0) === 0x80) cut--;
  return new TextDecoder().decode(bytes.slice(0, cut));
}

export function canonicalizeResponse(
  raw: string,
  itemIndex: number
): { lines: string[]; diags: ImportDiagnostic[] } {
  raw = raw.replaceAll('\r\n', '\n').replaceAll('\r', '\n');

  const diags: ImportDiagnostic[] = [];
  const lines = raw
    .split('\n')
    .map((piece) => chatLine(piece, itemIndex, diags))
    .filter((line): line is string => line !== null);

  if (lines.length > MAX_RESPONSE_LINES) {
    const extra = lines.length - MAX_RESPONSE_LINES;
    diags.push(
      warnDiag(
        itemIndex,
        CODE.responseLineDropped,
        `${extra} response line(s) dropped past the ${MAX_RESPONSE_LINES}-line limit`
      )
    );
    lines.length = MAX_RESPONSE_LINES;
  }
  return { lines, diags };
}

function chatLine(piece: string, itemIndex: number, diags: ImportDiagnostic[]): string | null {
  let line = piece.trim();
  if (line === '') return null;
  if (byteLen(line) > MAX_RESPONSE_LINE_BYTES) {
    const cut = truncateBytes(line, MAX_RESPONSE_LINE_BYTES);
    diags.push(
      warnDiag(
        itemIndex,
        CODE.responseTruncated,
        `response line cut from ${byteLen(line)} to ${byteLen(cut)} bytes (Twitch per-message limit)`
      )
    );
    line = cut;
  }
  return line;
}

export const PERM_TIERS: readonly Perm[] = ['everyone', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'];

const PERMISSION_ALIASES: Record<string, Perm> = {
  everyone: 'everyone',
  viewer: 'everyone',
  viewers: 'everyone',
  user: 'everyone',
  regular: 'everyone',
  regulars: 'everyone',
  follower: 'everyone',
  followers: 'everyone',
  subscriber: 'sub',
  subscribers: 'sub',
  sub: 'sub',
  subs: 'sub',
  vip: 'vip',
  vips: 'vip',
  twitch_vip: 'vip',
  moderator: 'mod',
  moderators: 'mod',
  mod: 'mod',
  mods: 'mod',
  lead_mod: 'lead_mod',
  broadcaster: 'broadcaster',
  streamer: 'broadcaster',
  owner: 'broadcaster'
};

export function mapPermission(raw: string): { perm: Perm; recognized: boolean } {
  const label = raw.trim().toLowerCase();
  if (label === '') return { perm: 'everyone', recognized: true };
  const mapped = PERMISSION_ALIASES[label];
  if (mapped) return { perm: mapped, recognized: true };
  return { perm: 'everyone', recognized: false };
}

const STAT_KEYS: readonly (keyof ImportStats)[] = ['commands', 'timers', 'triggers', 'quotes'];

export function stats(m: ImportManifest | null | undefined): ImportStats {
  const out = {} as ImportStats;
  for (const key of STAT_KEYS) {
    out[key] = m?.[key]?.length ?? 0;
  }
  return out;
}

export function isEmptyStats(s: ImportStats): boolean {
  return STAT_KEYS.every((key) => s[key] === 0);
}

export function findCollisions(existingNames: string[], m: ImportManifest | null | undefined): CollisionRef[] {
  if (!m || existingNames.length === 0) return [];
  const existing = new Set(existingNames.map(normalizeName));

  return [
    ...(m.commands ?? []).filter((c) => commandCollides(c, existing)).map((c) => collisionRef('command', c.name)),
    ...(m.fetches ?? []).filter((f) => existing.has(normalizeName(f.name))).map((f) => collisionRef('fetch', f.name))
  ];
}

function commandCollides(c: ManifestCommand, existing: Set<string>): boolean {
  if (existing.has(normalizeName(c.name))) return true;
  return (c.aliases ?? []).some((a) => existing.has(normalizeName(a)));
}

function collisionRef(kind: 'command' | 'fetch', name: string): CollisionRef {
  return { kind, name: normalizeName(name) };
}

const MAX_IMPORT_COMMANDS = IMPORT_ITEM_CAPS.commands;
const MAX_IMPORT_TIMERS = IMPORT_ITEM_CAPS.timers;
const MAX_IMPORT_TRIGGERS = IMPORT_ITEM_CAPS.triggers;
const MAX_IMPORT_QUOTES = IMPORT_ITEM_CAPS.quotes;

const MAX_QUOTE_TEXT_LEN = 450;
export const MIN_TIMER_INTERVAL_SECONDS = 30;
export const MAX_AUTOMOD_TERMS = 200;

const MAX_FETCH_SLUG_SUFFIX = 5;

export type FetchSlugSource = 'se' | 'moobot' | 'nightbot' | 'fossabot' | 'wizebot' | 'slcb';

export function fetchDefSlug(source: FetchSlugSource, commandName: string): string {
  const budget = FETCH_NAME_MAX - source.length - 1 - MAX_FETCH_SLUG_SUFFIX;
  return `${source}_${slugifyName(slugifyName(commandName).slice(0, budget))}`;
}

export function isValidFetchDefName(name: string): boolean {
  return name.length <= FETCH_NAME_MAX && FETCH_DEF_NAME_RE.test(name);
}

const FETCH_DEF_NAME_RE = /^[a-z0-9_]+$/;

const MAX_COMMAND_NAME_LEN = 64;
const MAX_COMMAND_ALIASES = 25;

function q(s: string): string {
  return JSON.stringify(s);
}

function errDiag(d: Omit<ImportDiagnostic, 'severity'>): ImportDiagnostic {
  return { severity: 'error', ...d };
}

export function warnDiag(itemIndex: number, code: string, message: string): ImportDiagnostic {
  return { severity: 'warn', item_index: itemIndex, code, message };
}

const NAME_RULE = 'command name must be 1-64 printable ASCII characters without spaces';

export function commandNameProblem(name: string): string | null {
  const n = byteLen(name);
  if (n === 0 || n > MAX_COMMAND_NAME_LEN) return NAME_RULE;
  return printableAscii(name) ? null : NAME_RULE;
}

function printableAscii(name: string): boolean {
  for (let i = 0; i < name.length; i++) {
    const c = name.charCodeAt(i);
    if (c <= 0x20 || c > 0x7e) return false;
  }
  return true;
}

const ALIAS_RULE = 'aliases must each be a valid command name, unique, and at most 25 in total';

function commandAliasesProblem(aliases: string[]): string | null {
  if (aliases.length > MAX_COMMAND_ALIASES) return ALIAS_RULE;
  const seen = new Set<string>();
  for (const alias of aliases) {
    if (aliasTaken(alias, seen)) return ALIAS_RULE;
    seen.add(alias.toLowerCase());
  }
  return null;
}

function aliasTaken(alias: string, seen: Set<string>): boolean {
  if (commandNameProblem(alias)) return true;
  return seen.has(alias.toLowerCase());
}

const RESPONSE_RULE =
  'command response must be 1-5 lines, each 1-500 characters without control characters';

function commandResponseProblem(response: string): string | null {
  if (byteLen(response) === 0) return RESPONSE_RULE;
  const lines = response.split('\n');
  if (lines.length > MAX_RESPONSE_LINES) return RESPONSE_RULE;
  return lines.every(validResponseLine) ? null : RESPONSE_RULE;
}

function validResponseLine(line: string): boolean {
  const n = byteLen(line);
  if (n === 0 || n > MAX_RESPONSE_LINE_BYTES) return false;
  return !hasControlChars(line);
}

function hasControlChars(line: string): boolean {
  for (const ch of line) {
    if ((ch.codePointAt(0) ?? 0) < 0x20) return true;
  }
  return false;
}

export function validateManifest(m: ImportManifest | null | undefined): ImportDiagnostic[] {
  if (!m) return [];
  const diags: ImportDiagnostic[] = [];
  if (isEmptyStats(stats(m)) && !m.automod)
    diags.push(warnDiag(-1, CODE.manifestEmpty, 'manifest carries no items'));

  for (const walk of KIND_WALKERS) diags.push(...walk(m));

  if (m.automod) diags.push(...automodDiags(m.automod));
  return diags;
}

interface CollectionKind<T> {
  noun: string;
  cap: number;
  overflowCode: string;
  items: (m: ImportManifest) => T[];
  validateItem: (item: T, index: number) => ImportDiagnostic[];
}

function walkKind<T>(kind: CollectionKind<T>): (m: ImportManifest) => ImportDiagnostic[] {
  return (m) =>
    kind.items(m).flatMap((item, i) =>
      i >= kind.cap ? [errDiag({ item_index: i, code: kind.overflowCode, message: `only the first ${kind.cap} ${kind.noun} are imported` })] : kind.validateItem(item, i)
    );
}

const KIND_WALKERS: ((m: ImportManifest) => ImportDiagnostic[])[] = [
  walkKind({
    noun: 'commands',
    cap: MAX_IMPORT_COMMANDS,
    overflowCode: CODE.commandTooMany,
    items: (m) => m.commands ?? [],
    validateItem: validateCommandItem
  }),
  walkKind({
    noun: 'timers',
    cap: MAX_IMPORT_TIMERS,
    overflowCode: CODE.timerTooMany,
    items: (m) => m.timers ?? [],
    validateItem: validateTimerItem
  }),
  walkKind({
    noun: 'triggers',
    cap: MAX_IMPORT_TRIGGERS,
    overflowCode: CODE.triggerTooMany,
    items: (m) => m.triggers ?? [],
    validateItem: validateTriggerItem
  }),
  walkKind({
    noun: 'quotes',
    cap: MAX_IMPORT_QUOTES,
    overflowCode: CODE.quoteTooMany,
    items: (m) => m.quotes ?? [],
    validateItem: validateQuoteItem
  })
];

function automodDiags(terms: NonNullable<ImportManifest['automod']>): ImportDiagnostic[] {
  if ((terms.block?.length ?? 0) <= MAX_AUTOMOD_TERMS && (terms.allow?.length ?? 0) <= MAX_AUTOMOD_TERMS) return [];
  return [
    warnDiag(-1, CODE.automodTermsTooMany, `automod term lists truncated to ${MAX_AUTOMOD_TERMS} entries per list at commit`)
  ];
}

interface CommandItem {
  command: ManifestCommand;
  name: string;
  index: number;
}

function validateCommandItem(c: ManifestCommand, index: number): ImportDiagnostic[] {
  const item: CommandItem = { command: c, name: normalizeName(c.name), index };
  return [
    ...commandNameDiags(item),
    ...commandAliasDiags(item),
    ...commandResponseDiags(item),
    ...commandTierDiags(c, index)
  ];
}

function commandNameDiags(item: CommandItem): ImportDiagnostic[] {
  const { command, name, index } = item;
  const problem = commandNameProblem(name);
  return problem ? [errDiag({ item_index: index, code: CODE.nameInvalid, message: `command name ${q(command.name)}: ${problem}` })] : [];
}

function commandAliasDiags(item: CommandItem): ImportDiagnostic[] {
  const { command, name, index } = item;
  if (!command.aliases?.length) return [];
  const problem = commandAliasesProblem(command.aliases.map(normalizeName));
  return problem ? [errDiag({ item_index: index, code: CODE.aliasInvalid, message: `aliases for ${q(name)}: ${problem}` })] : [];
}

function commandResponseDiags(item: CommandItem): ImportDiagnostic[] {
  const { command, name, index } = item;
  if (!command.responses?.length) return [errDiag({ item_index: index, code: CODE.responseInvalid, message: 'command has no response' })];
  const problem = commandResponseProblem(command.responses.join('\n'));
  return problem ? [errDiag({ item_index: index, code: CODE.responseInvalid, message: `response for ${q(name)}: ${problem}` })] : [];
}

function commandTierDiags(c: ManifestCommand, index: number): ImportDiagnostic[] {
  const out: ImportDiagnostic[] = [];
  if (c.permission && !PERM_TIERS.includes(c.permission)) {
    out.push(
      errDiag({
        item_index: index,
        code: CODE.permissionUnmapped,
        message: `permission ${q(c.permission)} is not one of everyone/sub/vip/mod/lead_mod/broadcaster`
      })
    );
  }
  if ((c.cooldown_seconds ?? 0) > MAX_COOLDOWN_SECONDS) {
    out.push(
      warnDiag(index, CODE.cooldownClamped, `cooldown ${c.cooldown_seconds}s clamped to ${MAX_COOLDOWN_SECONDS}s at commit`)
    );
  }
  return out;
}

function validateTimerItem(t: ManifestTimer, index: number): ImportDiagnostic[] {
  const out: ImportDiagnostic[] = [];
  if (t.message.trim() === '') out.push(errDiag({ item_index: index, code: CODE.timerMessageEmpty, message: 'timer has no message' }));
  if (t.interval_seconds < MIN_TIMER_INTERVAL_SECONDS) {
    out.push(
      warnDiag(
        index,
        CODE.intervalClamped,
        `interval ${t.interval_seconds}s clamped to ${MIN_TIMER_INTERVAL_SECONDS}s at commit (engine floor)`
      )
    );
  }
  return out;
}

function validateTriggerItem(tr: ManifestTrigger, index: number): ImportDiagnostic[] {
  if (tr.phrase.trim() !== '' && tr.response.trim() !== '') return [];
  return [
    errDiag({ item_index: index, code: CODE.triggerInvalid, message: `trigger needs both a phrase and a response; got phrase=${q(tr.phrase)}` })
  ];
}

function validateQuoteItem(qt: ManifestQuote, index: number): ImportDiagnostic[] {
  const out: ImportDiagnostic[] = [];
  const text = qt.text.trim();
  if (text === '' || byteLen(text) > MAX_QUOTE_TEXT_LEN) {
    out.push(errDiag({ item_index: index, code: CODE.quoteTextInvalid, message: `quote text must be 1-${MAX_QUOTE_TEXT_LEN} bytes` }));
  }
  if (qt.created_at && !isRFC3339(qt.created_at)) {
    out.push(errDiag({ item_index: index, code: CODE.quoteDateInvalid, message: 'created_at must be RFC 3339 (e.g. 2026-01-31T12:00:00Z)' }));
  }
  return out;
}

export function isRFC3339(s: string): boolean {
  const m = /^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})(\.\d+)?(?:[Zz]|[+-]\d{2}:\d{2})$/.exec(s);
  if (!m) return false;
  const day: CalendarDay = { y: +m[1], mo: +m[2], d: +m[3] };
  const time: ClockTime = { h: +m[4], mi: +m[5], s: +m[6] };
  return validCalendarDay(day) && validClock(time);
}

export interface CalendarDay {
  y: number;
  mo: number;
  d: number;
}

export interface ClockTime {
  h: number;
  mi: number;
  s: number;
}

export function validCalendarDay(day: CalendarDay): boolean {
  if (day.d < 1) return false;
  const days = monthLength(day.y, day.mo);
  if (days === null) return false;
  return day.d <= days;
}

function monthLength(y: number, mo: number): number | null {
  if (mo < 1 || mo > 12) return null;
  const daysInMonth = [31, isLeapYear(y) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return daysInMonth[mo - 1];
}

function isLeapYear(y: number): boolean {
  return (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0;
}

export function validClock(time: ClockTime): boolean {
  return time.h <= 23 && time.mi <= 59 && time.s <= 60;
}

export type FailedCollection = 'commands' | 'timers' | 'triggers' | 'quotes';

const FAILED_PREFIXES: readonly [prefix: string, collection: FailedCollection][] = [
  ['command', 'commands'],
  ['timer', 'timers'],
  ['trigger', 'triggers'],
  ['quote', 'quotes']
];

function failedCollection(code: string): FailedCollection | null {
  const hit = FAILED_PREFIXES.find(([prefix]) => code.startsWith(prefix));
  return hit ? hit[1] : null;
}

export class FailedItems {
  private readonly sets: Map<FailedCollection, Set<number>>;

  constructor(diags: ImportDiagnostic[]) {
    this.sets = new Map();
    for (const d of diags) {
      if (d.severity !== 'error' || d.item_index < 0) continue;
      const collection = failedCollection(d.code);
      if (!collection) continue;
      let set = this.sets.get(collection);
      if (!set) this.sets.set(collection, (set = new Set()));
      set.add(d.item_index);
    }
  }

  has(collection: FailedCollection, idx: number): boolean {
    return this.sets.get(collection)?.has(idx) ?? false;
  }
}
