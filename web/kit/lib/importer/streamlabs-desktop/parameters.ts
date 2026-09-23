// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// $parameter translation and the SLCB permission mapping: the port of
// parameters.go. Every SLCB-specific destination is decided here before the
// shared permission table is consulted.

import type { ImportDiagnostic } from '../types';
import { CODE, mapPermission, normalizeName } from '../validate';
import { emit, normalizeInstant, POSITIONAL_MAX, positional, slice } from '../targets';
import { parseFetchArgs } from '../nightbot/fetchdefs';
import type { FetchSlotSink } from '../nightbot/fetchdefs';
import { goAtoi } from './dbfile';
import { warnDiag } from './dbfile';

// every code is a row here so call sites never repeat raw strings.
export const SLCB_CODE = {
  manifestSourceNote: 'manifest_source_note',
  permissionAdjusted: 'command_permission_adjusted',
  scriptDependent: 'command_script_dependent',
  quoteDateUnparsed: 'quote_date_unparsed',
  countRemapped: 'command_count_remapped',
  positionalLowerLost: 'command_positional_lower_lost',
  positionalNumericLost: 'command_positional_numeric_lost'
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
//   $userid    the chatter's numeric/login id → {user.id} (phase 6 bug fix:
//              this used to fold onto {user}, the DISPLAY name — SLCB's own
//              Parameters wiki draws the same distinction our {user}/
//              {user.id} pair does, and a response built around an id
//              deserves the id, not a second spelling of the name)
//   $targetname/$tousername/$touser/$target → {touser} ($target is legacy
//              AnkhBot spelling)
//   $mychannel broadcaster channel → {channel}
//   $msg/$dummyormsg everything after the command → {args}
//   $argl      everything after the command, lower-cased → {args} (the
//              lower-casing itself has no equivalent; see the decision
//              record on $arglN below, which the bare form shares)
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
  // $randusername draws one name out of the people currently in chat —
  // {random.viewer}'s own definition.
  randusername: emit('random.viewer')!,
  // $points is the invoking chatter's own loyalty balance; $currencyname is
  // the broadcaster's name for that currency (SLCB's "Points name" setting).
  points: emit('points')!,
  currencyname: emit('points.name')!
};

// externalParams stay literal but mark the command script/API-dependent: their
// values come from HTTP calls, local files or wall-clock countdowns that have
// no import-time equivalent ($desc is stripped entirely instead, see
// translateVariables). Names follow the official Parameters wiki page.
// countdown/countup are NOT here (phase 6): they have their own PARAM_HANDLERS
// entries below, since — unlike the file/script calls in this set — a date
// argument that parses is a real, computable equivalent rather than a stay-
// literal case.
// readapi is NOT here (phase 6): it has its own PARAM_HANDLERS entry below,
// since — like countdown/countup above — a URL that parses is a real,
// computable equivalent ({urlfetch:<slug>}) rather than a stay-literal case.
// Every name still in this set reads or writes a local FILE on the machine
// SLCB itself ran on: file I/O has no filesystem here to read or write, so
// none of them has an equivalent at all, computable or otherwise.
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
  external: boolean; // used $readline/$savetofile/... style parameters (phase 6: $readapi no longer sets this — it maps to {urlfetch:…} instead, see readapiParam)
}

// ScanState threads one response through the per-parameter handlers below:
// pos is the index of the '$' being dispatched, out the output under
// construction, seenWarns the dedupe keys for emitted diagnostics, sink the
// per-command urlfetch slot allocator (undefined for timer messages, which
// carry no command name to build a slug from — readapiParam degrades to
// literal+warn without one, same as every other source's fetch handler).
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

// translateVariables rewrites SLCB $parameters in one response/timer message
// into this bot's {key} set, leaving anything unmappable as literal text plus
// a warn diagnostic naming the token: deleting a broadcaster's text silently
// is worse than a stray brace.
//
// cmdName gates $count resolution (bare {count}, the per-command use count —
// see uses.go's alias of {uses}); empty for timer messages, where $count has
// no command to count and stays literal. $count auto-increments per run
// upstream exactly like {count}/{uses} does here, which is why it maps onto
// that rather than a named {counter:<cmdName>}: a fresh import would
// otherwise silently create (and bind the command to) a channel counter the
// broadcaster never named. $checkcount(name), below, is SLCB's own
// NAMED-counter reference and is unaffected: it already read rather than
// wrote, so it keeps mapping onto {counter:<name>}.
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
      s.out += ch; // "$" with no identifier: literal dollar sign
      i++;
      continue;
    }
    s.pos = i;
    i = dispatchParam(s, cursor);
  }

  s.res.text = s.out;
  return s.res;
}

// dispatchParam routes one scanned $name to its rule: the SIMPLE_PARAMS map
// first, then PARAM_HANDLERS, then the predicate families (numbered arg slots,
// external calls), finally the unknown fallback. Adding a parameter later is a
// row in one of those tables, not a branch here.
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

// counterSpan folds one counter name into its span, or null when the name
// cannot survive our own lexer ('|' would turn the tail into a fallback, '}'
// would close the span early). A command name is printable ASCII, so both are
// reachable from a real export; the callers degrade to literal+warn rather
// than emitting a token that names a different counter than it reads.
function counterSpan(name: string): string | null {
  const norm = normalizeName(name);
  return norm === '' ? null : emit('counter', norm);
}

function countParam(s: ScanState, cursor: ParamCursor): number {
  const { next } = cursor;
  if (s.cmdName !== '') {
    s.out += emit('count')!;
    // Meaning change, not just a spelling change: SLCB's $count was a live
    // running total from whenever the command was created; {count} starts
    // counting this bot's own runs from zero. SLCB's own export carries no
    // running value to cite (unlike Moobot's <counter>, which at least has
    // one to name), so this warns without an old-value clause. warnOnce
    // dedupes by (code, message), so a response using $count more than once
    // still warns a single time.
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

// countdownParam builds the $countdown(d)/$countup(d) handler for one of the
// two names: a real mapping when d parses as a date (normalizeInstant reads
// SLCB's free-form spelling, e.g. "Dec 25 2026 12:00:00 PST", the same shape
// Nightbot exports — see targets.ts), literal+warn otherwise. Unlike the
// generic EXTERNAL_PARAMS path this replaces, an unparsable date still warns
// with the reason (not a script/file dependency, a date this bot could not
// read), because the fix here is something the broadcaster can act on.
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

// refuseReadapi is the shared "cannot use this call" exit: no argument, a
// blank one, or no sink to attach a definition to (timer messages carry no
// command name) all read the same to the broadcaster.
function refuseReadapi(s: ScanState, cursor: ParamCursor): number {
  s.out += '$readapi';
  warnOnce(s, CODE.variableUnmapped, '"$readapi(...)" is missing its URL, or has no command to attach a definition to; left as literal text');
  return cursor.next;
}

// readapiParam maps $readapi(URL) onto {urlfetch:<slug>} plus a synthesized
// definition shell carrying the URL, the same shape Moobot's/Nightbot's own
// $(urlfetch …)/$(customapi …) handlers use (nightbot/fetchdefs.ts's sink,
// parameterized by source — 'slcb' here). Without a sink (timer messages
// carry no command name to build a slug from) or with an unusable URL — most
// notably one that still contains another literal '$param' this bot could
// not translate first, which parseFetchArgs's usableUrl refuses on the same
// '$'/'{'/'}' check every other source's fetch parser shares — the call
// stays literal and warns, exactly like Nightbot's "URL built out of another
// variable is never baked into a definition".
function readapiParam(s: ScanState, cursor: ParamCursor): number {
  const arg = parenArg(s, cursor);
  if (arg === null || arg.trim() === '') return refuseReadapi(s, cursor);
  // Kept as its own guard (rather than folded into the check above) so the
  // narrowing below — s.sink.acquire(...) — does not need a non-null
  // assertion: TypeScript already knows s.sink is defined past this return.
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

// argsSlotParam resolves one $argN/$numN/$arglN in place, per slot: no more
// all-or-nothing (the old rule mapped a LONE $arg1 onto {args} and gave up
// entirely the moment a response used a second slot — deleted in phase 6, see
// the decision record on SIMPLE_PARAMS above for $argl's bare form). Bare
// arg/num slots are plain positional words ({N}); the 'l' family is SLCB's
// own lower-cased variant, mapped onto the REST-from-N slice ({N:}) per the
// phase 6 spec rather than a second positional word, and warned every time:
// this bot's positional words do not lower-case their text, and nothing else
// in the grammar can, so the case SLCB guaranteed is not guaranteed here.
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
  // $numN was SLCB's own numeric-only word slot — it read as its FALLBACK
  // (usually empty) when the word was not a number, where this bot's plain
  // positional word ({N}) prints whatever text is there whether or not it
  // parses as a number. Mapping both $argN and $numN onto the same {N} is
  // still the closest available token (there is no "positional word, numeric
  // only" span here), but the validation itself is lost, which is worth
  // telling the broadcaster rather than letting a non-numeric word appear
  // where SLCB would have printed nothing.
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
  // $desc(...) is a first-line metadata directive ("sync custom description to
  // the web" per SLCB docs), not response content; keeping it would post the
  // instruction into chat. Stripped when it opens the first line, kept literal
  // elsewhere.
  const endSpan = skipParensSpan(s, cursor);
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

function unknownParam(s: ScanState, cursor: ParamCursor): number {
  const { name, next } = cursor;
  const endSpan = skipParensSpan(s, cursor);
  s.out += `$${name}`;
  if (endSpan !== null) s.out += s.text.slice(next, endSpan);
  warnOnce(s, CODE.variableUnmapped, `response uses $${name}, which has no equivalent here; left as literal text`);
  return endSpan ?? next;
}

// scanIdent reads a $parameter identifier starting at start; returns the name
// and the index just past it. Identifiers begin with a letter or underscore:
// chat text like "$5" or "100$" must stay literal dollars, so a leading digit
// terminates the scan immediately.
// ParamCursor is a scanned $parameter: its identifier and the index just past
// it. Named because it threads through every handler below.
interface ParamCursor {
  name: string;
  next: number;
}

function scanIdent(text: string, start: number): ParamCursor {
  if (!hasIdentStart(text, start)) return { name: '', next: start };
  const end = readNameChars(text, start + 1);
  return { name: text.slice(start, end), next: end };
}

// hasIdentStart checks the first character after '$': identifiers begin with
// a letter or underscore: chat text like "$5" or "100$" must stay literal
// dollars, so a leading digit terminates the scan immediately.
function hasIdentStart(text: string, start: number): boolean {
  if (start >= text.length) return false;
  return isIdentLead(text[start]);
}

function isIdentLead(c: string): boolean {
  return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c === '_';
}

// readNameChars walks past the lead character while identifier characters
// continue, returning the exclusive end of the name.
function readNameChars(text: string, from: number): number {
  let i = from;
  while (i < text.length && isIdentChar(text[i])) i++;
  return i;
}


// parenArg returns the content of the (...) following the scanned parameter,
// or null. The helpers here all read the same two values the handlers already
// hold — the scan state and the cursor just past the parameter name — so they
// take those rather than a text/index pair unpacked at every call.
function parenArg(s: ScanState, cursor: ParamCursor): string | null {
  const end = skipParensSpan(s, cursor);
  if (end === null || end <= cursor.next + 2) return null;
  return s.text.slice(cursor.next + 1, end - 1);
}

// skipParensSpan returns the exclusive end index of the balanced parenthesis
// group starting at pos ('('), handling nesting; quotes inside are treated as
// plain characters, which matches how SLCB's own parameters nest.
function skipParensSpan(s: ScanState, cursor: ParamCursor): number | null {
  // The group has to open right where the parameter name ended; anything else
  // is a bare "$param" and the handlers keep it literal.
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

// skipParens returns the index just past the group at the cursor.
function skipParens(s: ScanState, cursor: ParamCursor): number {
  const end = skipParensSpan(s, cursor);
  return end ?? cursor.next;
}

// randnumTarget converts a $randnum(...) argument span "(...)" to
// {random:min-max}. One argument means 1..max; two means min,max (swapped when
// reversed, since {random:max-min} would be an empty range here while SLCB
// tolerates it).
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

// isIdentChar reports whether c continues a $parameter identifier.
function isIdentChar(c: string): boolean {
  return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c === '_';
}
