// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ImportDiagnostic } from '../types';
import { CODE, mapPermission, normalizeName } from '../validate';
import { emit, normalizeInstant, POSITIONAL_MAX, positional, slice } from '../targets';
import { parseFetchArgs } from '../nightbot/fetchdefs';
import type { FetchSlotSink } from '../nightbot/fetchdefs';
import { goAtoi } from './dbfile';
import { warnDiag } from './dbfile';

export const SLCB_CODE = {
  manifestSourceNote: 'manifest_source_note',
  permissionAdjusted: 'command_permission_adjusted',
  scriptDependent: 'command_script_dependent',
  quoteDateUnparsed: 'quote_date_unparsed',
  countRemapped: 'command_count_remapped',
  positionalLowerLost: 'command_positional_lower_lost',
  positionalNumericLost: 'command_positional_numeric_lost'
} as const;

export function mapPermissionSLCB(raw: string): { perm: string; diags: ImportDiagnostic[] } {
  const label = raw.trim().toLowerCase();
  return applyPermOutcome(
    SLCB_PERMS[label] ?? unknownSpelling(),
    raw
  );
}

type PermOutcome =
  | { kind: 'shared'; word: string }
  | { kind: 'adjusted'; perm: string; reason: (raw: string) => string }
  | { kind: 'unmapped'; reason: (raw: string) => string };

const sharedWith = (word: string): PermOutcome => ({ kind: 'shared', word });
const adjustedTo = (perm: string, reason: (raw: string) => string): PermOutcome => ({ kind: 'adjusted', perm, reason });

const SLCB_PERMS: Record<string, PermOutcome> = {
  '': sharedWith(''),
  '+a': sharedWith('everyone'),
  everyone: sharedWith('everyone'),
  '+s': sharedWith('subscriber'),
  subscriber: sharedWith('subscriber'),
  subscribers: sharedWith('subscriber'),
  sub: sharedWith('subscriber'),
  '+gw': adjustedTo('sub', (raw) => `GameWisp subscriber permission ${JSON.stringify(raw)} imported as sub`),
  'gamewisp subscriber': adjustedTo('sub', (raw) => `GameWisp subscriber permission ${JSON.stringify(raw)} imported as sub`),
  '+m': sharedWith('moderator'),
  moderator: sharedWith('moderator'),
  moderators: sharedWith('moderator'),
  mod: sharedWith('moderator'),
  vip: sharedWith('vip'),
  vips: sharedWith('vip'),
  streamer: sharedWith('streamer'),
  broadcaster: sharedWith('streamer'),
  owner: sharedWith('streamer'),
  caster: sharedWith('streamer'),
  '+e': adjustedTo('lead_mod', (raw) => `permission ${JSON.stringify(raw)} has no direct equivalent; mapped to lead_mod (both mean trusted staff above mods)`),
  editor: adjustedTo('lead_mod', (raw) => `permission ${JSON.stringify(raw)} has no direct equivalent; mapped to lead_mod (both mean trusted staff above mods)`),
  editors: adjustedTo('lead_mod', (raw) => `permission ${JSON.stringify(raw)} has no direct equivalent; mapped to lead_mod (both mean trusted staff above mods)`),
  '+r': widenedRegulars(),
  regular: widenedRegulars(),
  regulars: widenedRegulars()
};

function widenedRegulars(): PermOutcome {
  return {
    kind: 'unmapped',
    reason: (raw) => `permission ${JSON.stringify(raw)} (channel regulars) has no tier here; widened to everyone, tighten it after import if needed`
  };
}

function unknownSpelling(): PermOutcome {
  return {
    kind: 'unmapped',
    reason: (raw) => `permission ${JSON.stringify(raw)} is not one this importer knows; defaulted to everyone`
  };
}

function applyPermOutcome(outcome: PermOutcome, raw: string): { perm: string; diags: ImportDiagnostic[] } {
  switch (outcome.kind) {
    case 'shared':
      return sharedPerm(outcome.word);
    case 'adjusted':
      return {
        perm: outcome.perm,
        diags: [warnDiag(-1, SLCB_CODE.permissionAdjusted, outcome.reason(raw))]
      };
    case 'unmapped':
      return {
        perm: 'everyone',
        diags: [warnDiag(-1, CODE.permissionUnmapped, outcome.reason(raw))]
      };
  }
}

function sharedPerm(word: string): { perm: string; diags: ImportDiagnostic[] } {
  const { perm, recognized } = mapPermission(word);
  if (!recognized) {
    return {
      perm,
      diags: [
        warnDiag(
          -1,
          CODE.permissionUnmapped,
          `permission ${JSON.stringify(word)} did not resolve against the shared permission table; defaulted to everyone`
        )
      ]
    };
  }
  return { perm, diags: [] };
}

const SIMPLE_PARAMS: Record<string, string> = {
  username: emit('user')!,
  userid: emit('user.id')!,
  targetname: emit('touser')!,
  tousername: emit('touser')!,
  touser: emit('touser')!,
  target: emit('touser')!,
  mychannel: emit('channel')!,
  msg: emit('args')!,
  dummyormsg: emit('args')!,
  argl: emit('args')!,
  randusername: emit('random.viewer')!,
  points: emit('points')!,
  currencyname: emit('points.name')!
};

const EXTERNAL_PARAMS = new Set([
  'readline',
  'readrandline',
  'readspecificline',
  'savetofile',
  'overwritefile'
]);

interface TranslationResult {
  text: string;
  diags: ImportDiagnostic[];
  external: boolean;
}

interface ScanState {
  text: string;
  cmdName: string;
  res: TranslationResult;
  out: string;
  pos: number;
  seenWarns: Set<string>;
  sink?: FetchSlotSink;
}

function warnOnce(s: ScanState, code: string, message: string): void {
  const key = `${code}|${message}`;
  if (s.seenWarns.has(key)) return;
  s.seenWarns.add(key);
  s.res.diags.push(warnDiag(-1, code, message));
}

export function translateVariables(text: string, cmdName: string, sink?: FetchSlotSink): TranslationResult {
  const s: ScanState = {
    text,
    cmdName,
    res: { text: '', diags: [], external: false },
    out: '',
    pos: 0,
    seenWarns: new Set(),
    sink
  };

  let i = 0;
  while (i < text.length) {
    const ch = text[i];
    if (ch !== '$') {
      s.out += ch;
      i++;
      continue;
    }
    const cursor = scanIdent(text, i + 1);
    if (cursor.name === '') {
      s.out += ch;
      i++;
      continue;
    }
    s.pos = i;
    i = dispatchParam(s, cursor);
  }

  s.res.text = s.out;
  return s.res;
}

type ParamHandler = (s: ScanState, cursor: ParamCursor) => number;

const PARAM_HANDLERS: Record<string, ParamHandler> = {
  count: countParam,
  checkcount: checkCountParam,
  randnum: randnumParam,
  desc: descParam,
  countdown: countdownParam('countdown'),
  countup: countdownParam('countup'),
  readapi: readapiParam
};

function dispatchParam(s: ScanState, cursor: ParamCursor): number {
  const simple = SIMPLE_PARAMS[cursor.name];
  if (simple !== undefined) {
    s.out += simple;
    return cursor.next;
  }
  const handler = PARAM_HANDLERS[cursor.name];
  if (handler) return handler(s, cursor);
  if (isArgsSlot(cursor.name)) return argsSlotParam(s, cursor);
  if (EXTERNAL_PARAMS.has(cursor.name)) return externalParam(s, cursor);
  return unknownParam(s, cursor);
}

function counterSpan(name: string): string | null {
  const norm = normalizeName(name);
  return norm === '' ? null : emit('counter', norm);
}

function countParam(s: ScanState, cursor: ParamCursor): number {
  const { next } = cursor;
  if (s.cmdName !== '') {
    s.out += emit('count')!;
    warnOnce(
      s,
      SLCB_CODE.countRemapped,
      'response uses "$count", imported as {count}: it now counts this command’s own runs from zero, not the running total it showed in SLCB'
    );
    return next;
  }
  s.out += '$count';
  warnOnce(
    s,
    CODE.variableUnmapped,
    'response uses "$count", which counts uses of a specific command; timers have no command to count, left as literal text'
  );
  return next;
}

function checkCountParam(s: ScanState, cursor: ParamCursor): number {
  const { next } = cursor;
  const arg = parenArg(s, cursor);
  if (arg === null || arg.trim() === '') {
    s.out += '$checkcount';
    warnOnce(s, CODE.variableUnmapped, '"$checkcount(...)" is missing its argument; left as literal text');
    return next;
  }
  const span = counterSpan(arg);
  if (span === null) {
    s.out += '$checkcount';
    warnOnce(s, CODE.variableUnmapped, '"$checkcount(...)" names a counter this bot cannot spell; left as literal text');
    return next;
  }
  s.out += span;
  return skipParens(s, cursor);
}

function countdownParam(name: 'countdown' | 'countup'): ParamHandler {
  return (s: ScanState, cursor: ParamCursor): number => {
    const { next } = cursor;
    const arg = parenArg(s, cursor);
    if (arg === null || arg.trim() === '') {
      s.out += `$${name}`;
      warnOnce(s, CODE.variableUnmapped, `"$${name}(...)" is missing its date argument; left as literal text`);
      return next;
    }
    const normalized = normalizeInstant(arg);
    const span = normalized === null ? null : emit(name, normalized);
    if (span === null) {
      s.out += s.text.slice(s.pos, skipParens(s, cursor));
      warnOnce(
        s,
        CODE.variableUnmapped,
        `response uses $${name}(${JSON.stringify(arg)}), whose date this bot could not read; left as literal text`
      );
      return skipParens(s, cursor);
    }
    s.out += span;
    return skipParens(s, cursor);
  };
}

function refuseReadapi(s: ScanState, cursor: ParamCursor): number {
  s.out += '$readapi';
  warnOnce(s, CODE.variableUnmapped, '"$readapi(...)" is missing its URL, or has no command to attach a definition to; left as literal text');
  return cursor.next;
}

function readapiParam(s: ScanState, cursor: ParamCursor): number {
  const arg = parenArg(s, cursor);
  if (arg === null || arg.trim() === '') return refuseReadapi(s, cursor);
  if (!s.sink) return refuseReadapi(s, cursor);
  const parsed = parseFetchArgs(arg.trim());
  if (!parsed) {
    s.out += s.text.slice(s.pos, skipParens(s, cursor));
    warnOnce(
      s,
      CODE.variableUnmapped,
      `response uses $readapi(${JSON.stringify(arg.trim())}), whose URL this bot cannot use; left as literal text`
    );
    return skipParens(s, cursor);
  }
  const key = s.sink.acquire(parsed.url);
  const span = key === null ? null : emit('urlfetch', key);
  if (span === null) {
    s.out += s.text.slice(s.pos, skipParens(s, cursor));
    warnOnce(s, CODE.variableUnmapped, '"$readapi(...)" could not be synthesized into a fetch definition; left as literal text');
    return skipParens(s, cursor);
  }
  s.out += span;
  return skipParens(s, cursor);
}

function randnumParam(s: ScanState, cursor: ParamCursor): number {
  const { next } = cursor;
  const endSpan = skipParensSpan(s, cursor);
  if (endSpan === null) {
    s.out += '$randnum';
    warnOnce(s, CODE.variableUnmapped, '"$randnum" is missing its (min,max) arguments; left as literal text');
    return next;
  }
  const target = randnumTarget(s.text.slice(next, endSpan));
  if (target !== null) {
    s.out += target;
    return endSpan;
  }
  s.out += s.text.slice(s.pos, endSpan);
  warnOnce(
    s,
    CODE.variableUnmapped,
    `response uses ${JSON.stringify(s.text.slice(s.pos, endSpan))}, whose range is not two numbers; left as literal text`
  );
  return endSpan;
}

function argsSlotParam(s: ScanState, cursor: ParamCursor): number {
  const { name, next } = cursor;
  const n = argsSlotIndex(name);
  const lowered = name.startsWith('argl');
  const span = lowered ? slice(n) : positional(n);
  if (span === null) {
    s.out += `$${name}`;
    warnOnce(
      s,
      CODE.variableUnmapped,
      `response uses $${name}, past the ${POSITIONAL_MAX}-word limit this bot's positional words read; left as literal text`
    );
    return next;
  }
  s.out += span;
  if (lowered) {
    warnOnce(
      s,
      SLCB_CODE.positionalLowerLost,
      `response uses $${name}, imported as ${span}: SLCB lower-cased this argument, this bot's positional words do not`
    );
  }
  if (name.startsWith('num')) {
    warnOnce(
      s,
      SLCB_CODE.positionalNumericLost,
      `response uses $${name}, imported as ${span}: SLCB only filled this in when the word was a number, this bot's positional words fill in whatever word is there`
    );
  }
  return next;
}

function externalParam(s: ScanState, cursor: ParamCursor): number {
  const { name, next } = cursor;
  s.res.external = true;
  s.out += `$${name}`;
  const endSpan = skipParensSpan(s, cursor);
  if (endSpan === null) return next;
  s.out += s.text.slice(next, endSpan);
  return endSpan;
}

function descParam(s: ScanState, cursor: ParamCursor): number {
  const { next } = cursor;
  const endSpan = skipParensSpan(s, cursor);
  if (endSpan === null) {
    s.out += '$desc';
    return next;
  }
  if (s.out.trim() === '') return swallowDirectiveBreak(s.text, endSpan);
  s.out += `$desc${s.text.slice(next, endSpan)}`;
  return endSpan;
}

function swallowDirectiveBreak(text: string, at: number): number {
  if (text[at] === '\n') return at + 1;
  if (text[at] === '\r' && text[at + 1] === '\n') return at + 2;
  return at;
}

function unknownParam(s: ScanState, cursor: ParamCursor): number {
  const { name, next } = cursor;
  const endSpan = skipParensSpan(s, cursor);
  s.out += `$${name}`;
  if (endSpan !== null) s.out += s.text.slice(next, endSpan);
  warnOnce(s, CODE.variableUnmapped, `response uses $${name}, which has no equivalent here; left as literal text`);
  return endSpan ?? next;
}

interface ParamCursor {
  name: string;
  next: number;
}

function scanIdent(text: string, start: number): ParamCursor {
  if (!hasIdentStart(text, start)) return { name: '', next: start };
  const end = readNameChars(text, start + 1);
  return { name: text.slice(start, end), next: end };
}

function hasIdentStart(text: string, start: number): boolean {
  if (start >= text.length) return false;
  return isIdentLead(text[start]);
}

function isIdentLead(c: string): boolean {
  return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c === '_';
}

function readNameChars(text: string, from: number): number {
  let i = from;
  while (i < text.length && isIdentChar(text[i])) i++;
  return i;
}


function parenArg(s: ScanState, cursor: ParamCursor): string | null {
  const end = skipParensSpan(s, cursor);
  if (end === null || end <= cursor.next + 2) return null;
  return s.text.slice(cursor.next + 1, end - 1);
}

function skipParensSpan(s: ScanState, cursor: ParamCursor): number | null {
  const { text } = s;
  if (text[cursor.next] !== '(') return null;
  let depth = 0;
  for (let i = cursor.next; i < text.length; i++) {
    depth += parenStep(text[i]);
    if (depth === 0) return i + 1;
  }
  return null;
}

function parenStep(ch: string): number {
  if (ch === '(') return 1;
  return ch === ')' ? -1 : 0;
}

function skipParens(s: ScanState, cursor: ParamCursor): number {
  const end = skipParensSpan(s, cursor);
  return end ?? cursor.next;
}

function randnumTarget(span: string): string | null {
  const parts = span.replace(/^\(/, '').replace(/\)$/, '').split(',');
  if (parts.length === 1) return singleBoundRange(parts[0]);
  if (parts.length === 2) return boundedRange(parts as [string, string]);
  return null;
}

function singleBoundRange(rawMax: string): string | null {
  const max = goAtoi(rawMax.trim());
  return max === null ? null : emit('random', `1-${max}`);
}

function boundedRange(parts: [string, string]): string | null {
  const a = goAtoi(parts[0].trim());
  const b = goAtoi(parts[1].trim());
  if (a === null || b === null) return null;
  return emit('random', `${Math.min(a, b)}-${Math.max(a, b)}`);
}

function isArgsSlot(name: string): boolean {
  if (name.startsWith('argl')) return name.length >= 5;
  return (name.startsWith('arg') || name.startsWith('num')) && name.length >= 4;
}

function argsSlotIndex(name: string): number {
  for (let i = name.length - 1; i >= 0; i--) {
    if (name[i] < '0' || name[i] > '9') return Number(name.slice(i + 1));
  }
  return 0;
}

function isIdentChar(c: string): boolean {
  return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c === '_';
}
