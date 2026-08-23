// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// $parameter translation and the SLCB permission mapping — the port of
// parameters.go. Every SLCB-specific destination is decided here before the
// shared permission table is consulted.

import type { ImportDiagnostic } from '../types';
import { CODE, mapPermission, normalizeName } from '../validate';
import { goAtoi } from './dbfile';
import { warnDiag } from './dbfile';

// every code is a row here so call sites never repeat raw strings.
export const SLCB_CODE = {
  manifestSourceNote: 'manifest_source_note',
  permissionAdjusted: 'command_permission_adjusted',
  scriptDependent: 'command_script_dependent',
  quoteDateUnparsed: 'quote_date_unparsed'
} as const;

// --- $parameter translation (parameters.go) -----------------------------------

// mapPermissionSLCB translates an SLCB permission value into a perm tier.
//
// SLCB's own tiers (its official "Permissions & Usage" wiki page) are the
// letter flags +a Everyone, +r Regular, +s Subscriber, +gw GameWisp Subscriber,
// +m Moderator, +e Editor, +i Invisible, plus user/min-rank/min-points/min-hours
// gates (+u/+r(rank)/+p/+h). Word spellings are what the desktop UI persists.
//
// Everything SLCB-specific is handled here BEFORE falling back to the shared
// permission table, because each case has a deliberate destination:
//   - regular → everyone + warn. Our tier set has no regular level (same call
//     the Moobot parser documents); widening beats dropping the command.
//   - editor → lead_mod + warn. A Twitch channel editor is trusted staff above
//     moderators, which is exactly this bot's lead_mod; mapping to mod would
//     narrow, leaving unmapped would widen to everyone.
//   - gamewisp subscriber → sub + warn (paid-subscriber equivalent).
//   - invisible / user / min-rank / min-points / min-hours gates → everyone +
//     warn: these gate on state we cannot express.
export function mapPermissionSLCB(raw: string): { perm: string; diags: ImportDiagnostic[] } {
  const label = raw.trim().toLowerCase();
  return applyPermOutcome(
    SLCB_PERMS[label] ?? unknownSpelling(),
    raw
  );
}

// PermOutcome is one row of the SLCB permission table. Shared spellings run
// through the repo-wide permission table so SLCB never diverges from how other
// parsers land tiers; adjusted/unmapped rows carry their deliberate
// destination plus the diagnostic explaining it.
//
// Everything SLCB-specific is handled here BEFORE falling back to the shared
// table, because each case has a deliberate destination:
//   - regular → everyone + warn. Our tier set has no regular level (same call
//     the Moobot parser documents); widening beats dropping the command.
//   - editor → lead_mod + warn. A Twitch channel editor is trusted staff above
//     moderators, which is exactly this bot's lead_mod; mapping to mod would
//     narrow, leaving unmapped would widen to everyone.
//   - gamewisp subscriber → sub + warn (paid-subscriber equivalent).
//   - invisible / user / min-rank / min-points / min-hours gates → everyone +
//     warn: these gate on state we cannot express.
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

// widenedRegulars lands on everyone with the unmapped code: channel regulars
// have no tier here, so the mapping layer's widening fallback applies.
function widenedRegulars(): PermOutcome {
  return {
    kind: 'unmapped',
    reason: (raw) => `permission ${JSON.stringify(raw)} (channel regulars) has no tier here; widened to everyone — tighten it after import if needed`
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

// sharedPerm runs a canonical word spelling through the repo-wide permission
// table so SLCB never diverges from how other parsers land tiers.
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

// simpleParams are the clean 1:1 mappings:
//   $username  invoking chatter display name → {user}
//   $userid    login-lowercase name → {user}
//   $targetname/$tousername/$touser/$target → {target} ($target is legacy
//              AnkhBot spelling)
//   $mychannel broadcaster channel → {channel}
//   $msg/$dummyormsg everything after the command → {args}
const SIMPLE_PARAMS: Record<string, string> = {
  username: '{user}',
  userid: '{user}',
  targetname: '{target}',
  tousername: '{target}',
  touser: '{target}',
  target: '{target}',
  mychannel: '{channel}',
  msg: '{args}',
  dummyormsg: '{args}'
};

// externalParams stay literal but mark the command script/API-dependent: their
// values come from HTTP calls, local files or wall-clock countdowns that have
// no import-time equivalent ($desc is stripped entirely instead — see
// translateVariables). Names follow the official Parameters wiki page.
const EXTERNAL_PARAMS = new Set([
  'readapi',
  'readline',
  'readrandline',
  'readspecificline',
  'savetofile',
  'overwritefile',
  'countdown',
  'countup'
]);

interface TranslationResult {
  text: string;
  diags: ImportDiagnostic[];
  external: boolean; // used $readapi/$readline/... style parameters
}

// ScanState threads one response through the per-parameter handlers below:
// pos is the index of the '$' being dispatched, out the output under
// construction, argMax the highest $argN/$numN index seen (drives the
// arg-slot rule), seenWarns the dedupe keys for emitted diagnostics.
interface ScanState {
  text: string;
  cmdName: string;
  res: TranslationResult;
  out: string;
  pos: number;
  argMax: number;
  seenWarns: Set<string>;
}

function warnOnce(s: ScanState, code: string, message: string): void {
  const key = `${code}|${message}`;
  if (s.seenWarns.has(key)) return;
  s.seenWarns.add(key);
  s.res.diags.push(warnDiag(-1, code, message));
}

// translateVariables rewrites SLCB $parameters in one response/timer message
// into this bot's {key} set, leaving anything unmappable as literal text plus
// a warn diagnostic naming the token: deleting a broadcaster's text silently
// is worse than a stray brace.
//
// cmdName is the normalized command name for $count resolution
// ({counter:name}); empty for timer messages, where $count has no referent and
// stays literal.
export function translateVariables(text: string, cmdName: string): TranslationResult {
  const s: ScanState = {
    text,
    cmdName,
    res: { text: '', diags: [], external: false },
    out: '',
    pos: 0,
    argMax: 0,
    seenWarns: new Set()
  };

  let i = 0;
  while (i < text.length) {
    const ch = text[i];
    if (ch !== '$') {
      s.out += ch;
      i++;
      continue;
    }
    const [name, next] = scanIdent(text, i + 1);
    if (name === '') {
      s.out += ch; // "$" with no identifier: literal dollar sign
      i++;
      continue;
    }
    s.pos = i;
    i = dispatchParam(s, name, next);
  }

  // Arg-slot rule: a lone $arg1/$num1 IS "everything after the command" and
  // rewrites to {args}; any higher index means the command splits positional
  // words we cannot express, so every slot stays literal with one shared
  // explanation.
  let final = s.out;
  if (s.argMax === 1) {
    final = replaceWord(final, ['arg1', 'num1', 'argl1'], '{args}');
  } else if (s.argMax > 1) {
    warnOnce(
      s,
      CODE.variableUnmapped,
      `response uses numbered argument slots up to $arg${s.argMax}; positional splitting has no equivalent, all slots left as literal text`
    );
  }

  s.res.text = final;
  return s.res;
}

// dispatchParam routes one scanned $name to its rule: the SIMPLE_PARAMS map
// first, then PARAM_HANDLERS, then the predicate families (numbered arg slots,
// external calls), finally the unknown fallback. Adding a parameter later is a
// row in one of those tables, not a branch here.
type ParamHandler = (s: ScanState, name: string, next: number) => number;

const PARAM_HANDLERS: Record<string, ParamHandler> = {
  count: countParam,
  checkcount: checkCountParam,
  randnum: randnumParam,
  desc: descParam
};

function dispatchParam(s: ScanState, name: string, next: number): number {
  const simple = SIMPLE_PARAMS[name];
  if (simple !== undefined) {
    s.out += simple;
    return next;
  }
  const handler = PARAM_HANDLERS[name];
  if (handler) return handler(s, name, next);
  if (isArgsSlot(name)) return argsSlotParam(s, name, next);
  if (EXTERNAL_PARAMS.has(name)) return externalParam(s, name, next);
  return unknownParam(s, name, next);
}

function countParam(s: ScanState, _name: string, next: number): number {
  if (s.cmdName !== '') {
    s.out += `{counter:${normalizeName(s.cmdName)}}`;
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

function checkCountParam(s: ScanState, _name: string, next: number): number {
  const arg = parenArg(s.text, next);
  if (arg === null || arg.trim() === '') {
    s.out += '$checkcount';
    warnOnce(s, CODE.variableUnmapped, '"$checkcount(...)" is missing its argument; left as literal text');
    return next;
  }
  s.out += `{counter:${normalizeName(arg)}}`;
  return skipParens(s.text, next);
}

function randnumParam(s: ScanState, _name: string, next: number): number {
  const endSpan = skipParensSpan(s.text, next);
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

function argsSlotParam(s: ScanState, name: string, next: number): number {
  const n = argsSlotIndex(name);
  if (n > s.argMax) s.argMax = n;
  s.out += `$${name}`; // provisionally literal; resolved by the arg-slot rule
  return next;
}

function externalParam(s: ScanState, name: string, next: number): number {
  s.res.external = true;
  s.out += `$${name}`;
  const endSpan = skipParensSpan(s.text, next);
  if (endSpan === null) return next;
  s.out += s.text.slice(next, endSpan);
  return endSpan;
}

function descParam(s: ScanState, _name: string, next: number): number {
  // $desc(...) is a first-line metadata directive ("sync custom description to
  // the web" per SLCB docs), not response content; keeping it would post the
  // instruction into chat. Stripped when it opens the first line, kept literal
  // elsewhere.
  const endSpan = skipParensSpan(s.text, next);
  if (endSpan === null) {
    s.out += '$desc';
    return next;
  }
  if (s.out.trim() === '') return swallowDirectiveBreak(s.text, endSpan);
  s.out += `$desc${s.text.slice(next, endSpan)}`;
  return endSpan;
}

// swallowDirectiveBreak eats the break directly after a leading $desc(...)
// directive so stripping it does not leave a blank first line.
function swallowDirectiveBreak(text: string, at: number): number {
  if (text[at] === '\n') return at + 1;
  if (text[at] === '\r' && text[at + 1] === '\n') return at + 2;
  return at;
}

function unknownParam(s: ScanState, name: string, next: number): number {
  const endSpan = skipParensSpan(s.text, next);
  s.out += `$${name}`;
  if (endSpan !== null) s.out += s.text.slice(next, endSpan);
  warnOnce(s, CODE.variableUnmapped, `response uses $${name}, which has no equivalent here; left as literal text`);
  return endSpan ?? next;
}

// scanIdent reads a $parameter identifier starting at start; returns the name
// and the index just past it. Identifiers begin with a letter or underscore —
// chat text like "$5" or "100$" must stay literal dollars, so a leading digit
// terminates the scan immediately.
function scanIdent(text: string, start: number): [name: string, next: number] {
  let i = start;
  if (i >= text.length) return ['', start];
  const c = text[i];
  const isLead = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c === '_';
  if (!isLead) return ['', start];
  while (i < text.length) {
    const ch = text[i];
    if ((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch === '_') {
      i++;
      continue;
    }
    break;
  }
  return [text.slice(start, i), i];
}

// parenArg returns the content of the (...) following pos, or null.
function parenArg(text: string, pos: number): string | null {
  const end = skipParensSpan(text, pos);
  if (end === null || end <= pos + 2) return null;
  return text.slice(pos + 1, end - 1);
}

// skipParensSpan returns the exclusive end index of the balanced parenthesis
// group starting at pos ('('), handling nesting; quotes inside are treated as
// plain characters, which matches how SLCB's own parameters nest.
function skipParensSpan(text: string, pos: number): number | null {
  if (!opensGroup(text, pos)) return null;
  let depth = 0;
  for (let i = pos; i < text.length; i++) {
    depth += parenStep(text[i]);
    if (depth === 0) return i + 1;
  }
  return null;
}

function opensGroup(text: string, pos: number): boolean {
  return pos < text.length && text[pos] === '(';
}

function parenStep(ch: string): number {
  if (ch === '(') return 1;
  return ch === ')' ? -1 : 0;
}

// skipParens returns the index just past the group at pos.
function skipParens(text: string, pos: number): number {
  const end = skipParensSpan(text, pos);
  return end ?? pos;
}

// randnumTarget converts a $randnum(...) argument span "(...)" to
// {random:min-max}. One argument means 1..max; two means min,max (swapped when
// reversed, since {random:max-min} would be an empty range here while SLCB
// tolerates it).
function randnumTarget(span: string): string | null {
  const parts = span.replace(/^\(/, '').replace(/\)$/, '').split(',');
  if (parts.length === 1) return singleBoundRange(parts[0]);
  if (parts.length === 2) return boundedRange(parts[0], parts[1]);
  return null;
}

function singleBoundRange(rawMax: string): string | null {
  const max = goAtoi(rawMax.trim());
  return max === null ? null : `{random:1-${max}}`;
}

function boundedRange(rawA: string, rawB: string): string | null {
  const a = goAtoi(rawA.trim());
  const b = goAtoi(rawB.trim());
  if (a === null || b === null) return null;
  return `{random:${Math.min(a, b)}-${Math.max(a, b)}}`;
}

// isArgsSlot reports whether name is one of $arg1..9 / $num1..9 / $argl1..9.
function isArgsSlot(name: string): boolean {
  if (name.startsWith('argl')) return name.length >= 5;
  return (name.startsWith('arg') || name.startsWith('num')) && name.length >= 4;
}

// argsSlotIndex extracts the digit suffix of a validated arg-slot name.
function argsSlotIndex(name: string): number {
  for (let i = name.length - 1; i >= 0; i--) {
    if (name[i] < '0' || name[i] > '9') return Number(name.slice(i + 1));
  }
  return 0;
}

// replaceWord rewrites exact $name occurrences (never prefixes of longer names
// such as $arg10) in already-scanned output text.
function replaceWord(text: string, names: string[], target: string): string {
  for (const n of names) {
    const token = `$${n}`;
    let b = '';
    for (;;) {
      const idx = text.indexOf(token);
      if (idx < 0) {
        b += text;
        break;
      }
      const after = idx + token.length;
      if (after < text.length && isIdentChar(text[after])) {
        b += text.slice(0, after); // longer identifier; keep scanning past it
        text = text.slice(after);
        continue;
      }
      b += text.slice(0, idx);
      b += target;
      text = text.slice(after);
    }
    text = b;
  }
  return text;
}

// isIdentChar reports whether c continues a $parameter identifier.
function isIdentChar(c: string): boolean {
  return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c === '_';
}
