// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Canonicalization + validation layer for config imports, ported one-for-one
// from app/importer/mapping (mapping.go, response.go, permission.go,
// validate.go) when the standalone importer service was folded into the
// dashboard. Every function here is pure and deterministic — parsers run it at
// parse time and the commit path runs it again before writing, so its outputs
// are wire-stable and pinned by tests (validate.test.ts replays the Go
// package's committed golden fixture as a parity check).
//
// The Go validators this layer called (internal/domain/validate CommandName /
// CommandAliases / CommandResponse / Perm) are inlined below with their exact
// error strings, because those strings surface verbatim inside diagnostics on
// the preview screen. FloorClean was NOT ported: the importer never set
// validate.CheckFloor, so it could not fire here; immovable-floor content is
// still refused by the commands service when commit writes each row.

import type {
  CollisionRef,
  ImportDiagnostic,
  ImportManifest,
  ImportStats,
  ManifestCommand,
  ManifestCounter,
  ManifestQuote,
  ManifestTimer,
  ManifestTrigger,
  Perm
} from './types';
import { IMPORT_ITEM_CAPS } from './types';

// --- diagnostic codes (restated from internal/domain/rpc/importer — kept in
// step; snake_case, item-kind-prefixed for item-level findings) ---------------
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
  counterNameInvalid: 'counter_name_invalid',
  automodTermsTooMany: 'automod_terms_too_many',
  moduleReadFailed: 'module_read_failed',
  writeFailed: 'write_failed',
  commandTooMany: 'command_too_many',
  timerTooMany: 'timer_too_many',
  triggerTooMany: 'trigger_too_many',
  quoteTooMany: 'quote_too_many',
  counterTooMany: 'counter_too_many'
} as const;

// maxResponseLineLength mirrors Twitch's per-message limit: a longer line would
// be silently eaten downstream. The ceiling belongs to Twitch, not to us.
export const MAX_RESPONSE_LINE_BYTES = 500;
export const MAX_RESPONSE_LINES = 5;
// Cooldown ceiling (one day) must match the commands service's validation.
export const MAX_COOLDOWN_SECONDS = 86400;

const encoder = new TextEncoder();
function byteLen(s: string): number {
  return encoder.encode(s).length;
}

// NormalizeName canonicalizes one command name the way the commands service's
// write hook does: trim, strip ONE leading "!", trim, lowercase. Chat carries
// the "!"; storage and lookup keys never do, so both spellings must fold onto
// the same key here or collision detection misses what the unique index would
// catch at write time.
export function normalizeName(name: string): string {
  return name.trim().replace(/^!/, '').trim().toLowerCase();
}

// ClampCooldown folds a source-provided cooldown onto the domain the commands
// service accepts: negative values mean "unset" upstream and become 0, values
// past the 86400s ceiling are clamped rather than rejected because every source
// UI lets users type arbitrary numbers and losing the command over a silly
// cooldown helps nobody.
export function clampCooldown(seconds: number): number {
  if (seconds <= 0) return 0;
  if (seconds > MAX_COOLDOWN_SECONDS) return MAX_COOLDOWN_SECONDS;
  return seconds;
}

// truncateBytes cuts s to at most limit bytes without splitting a UTF-8 rune,
// so a truncated emote or accented letter never becomes invalid UTF-8 on the
// wire (the bot posts these lines verbatim).
function truncateBytes(s: string, limit: number): string {
  const bytes = encoder.encode(s);
  if (bytes.length <= limit) return s;
  let cut = limit;
  while (cut > 0 && (bytes[cut] & 0xc0) === 0x80) cut--;
  return new TextDecoder().decode(bytes.slice(0, cut));
}

// CanonicalizeResponse splits one source response into chat-ready lines: CRLF
// folded to LF, surrounding whitespace trimmed, blank lines dropped, each
// remaining line capped at 500 bytes and the total capped at 5 lines. Every
// lossy fix is reported as a warn diagnostic attributed to itemIndex.
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

// chatLine trims one source line into a chat-ready line, truncating past the
// per-message byte limit with a diagnostic; blank lines vanish.
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

// --- permissions -------------------------------------------------------------

const PERM_TIERS: readonly Perm[] = ['everyone', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'];

// permissionAliases maps one external bot's permission labels onto this bot's
// perm tiers. Keys are lower-cased source spellings; plural and abbreviation
// variants observed in each product's UI/export are listed explicitly rather
// than fuzzy-matched, so an unknown label fails loud (recognized=false) instead
// of silently landing on the wrong tier:
//   - StreamElements: Everyone / Moderator / Owner (dashboard wording).
//   - Fossabot: Viewer / Subscriber / VIP / Moderator / Broadcaster|Streamer.
//   - Moobot: Everyone / Subscribers / Moderators / Owner plus Regulars.
//   - StreamLabs Desktop: Everyone / Subscriber / Moderator / Streamer (+VIP).
// lead_mod exists only in this bot; no source produces it, so it has no alias.
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
  moderator: 'mod',
  moderators: 'mod',
  mod: 'mod',
  mods: 'mod',
  lead_mod: 'lead_mod',
  broadcaster: 'broadcaster',
  streamer: 'broadcaster',
  owner: 'broadcaster'
};

// MapPermission translates one source permission label into a perm tier. An
// empty raw means the source had no permission field at all: that is the
// source's own default of everyone, so it returns (everyone, true). A non-empty
// raw with no table entry returns (everyone, false) so the parser can attach a
// permission_unmapped warning; defaulting to everyone is deliberate because
// every alternative either narrows a broadcaster's intent or invents trust.
export function mapPermission(raw: string): { perm: Perm; recognized: boolean } {
  const label = raw.trim().toLowerCase();
  if (label === '') return { perm: 'everyone', recognized: true };
  const mapped = PERMISSION_ALIASES[label];
  if (mapped) return { perm: mapped, recognized: true };
  return { perm: 'everyone', recognized: false };
}

// --- stats + collisions ------------------------------------------------------

// Stats tallies one manifest by collection. It counts what the manifest holds,
// regardless of validity — preview renders this number, commit computes its own
// applied tally from what actually wrote.
export function stats(m: ImportManifest | null | undefined): ImportStats {
  return {
    commands: m?.commands?.length ?? 0,
    timers: m?.timers?.length ?? 0,
    triggers: m?.triggers?.length ?? 0,
    quotes: m?.quotes?.length ?? 0,
    counters: m?.counters?.length ?? 0
  };
}

export function isEmptyStats(s: ImportStats): boolean {
  return s.commands === 0 && s.timers === 0 && s.triggers === 0 && s.quotes === 0 && s.counters === 0;
}

// FindCollisions returns the manifest items whose normalized name (or alias)
// matches an entry of existingNames. Counters use the same normalization the
// loyalty service applies to counter keys (lower-cased, no leading "!").
export function findCollisions(existingNames: string[], m: ImportManifest | null | undefined): CollisionRef[] {
  if (!m || existingNames.length === 0) return [];
  const existing = new Set(existingNames.map(normalizeName));

  return [
    ...(m.commands ?? []).filter((c) => commandCollides(c, existing)).map((c) => collisionRef('command', c.name)),
    ...(m.counters ?? []).filter((c) => existing.has(normalizeName(c.name))).map((c) => collisionRef('counter', c.name))
  ];
}

// A command collides when its own normalized name OR any alias matches an
// existing entry.
function commandCollides(c: ManifestCommand, existing: Set<string>): boolean {
  if (existing.has(normalizeName(c.name))) return true;
  return (c.aliases ?? []).some((a) => existing.has(normalizeName(a)));
}

function collisionRef(kind: 'command' | 'counter', name: string): CollisionRef {
  return { kind, name: normalizeName(name) };
}

// --- whole-manifest validation -----------------------------------------------

// Caps mirror IMPORT_ITEM_CAPS exactly; restated locally so the cap, its
// diagnostics and the client truncation read together (asserted equal by test).
const MAX_IMPORT_COMMANDS = IMPORT_ITEM_CAPS.commands;
const MAX_IMPORT_TIMERS = IMPORT_ITEM_CAPS.timers;
const MAX_IMPORT_TRIGGERS = IMPORT_ITEM_CAPS.triggers;
const MAX_IMPORT_QUOTES = IMPORT_ITEM_CAPS.quotes;
const MAX_IMPORT_COUNTERS = IMPORT_ITEM_CAPS.counters;

// maxQuoteTextLen mirrors the quote column cap in the modules service: the
// quote readout prepends "Quote #N: " and appends " (date)" inside one Twitch
// message, so the schema holds the body under 450 rather than 500.
const MAX_QUOTE_TEXT_LEN = 450;
// minTimerIntervalSeconds re-states sesame's engine floor (30s): below it a
// timer arms an expire/fire/re-arm loop the engine refuses, so import clamps
// instead of writing a timer that silently never fires.
export const MIN_TIMER_INTERVAL_SECONDS = 30;
// Bounds one term list inside the modules service's 16KiB config-blob cap with
// headroom for the merged blob (2 x 200 terms x ~100 bytes), so hitting the cap
// mid-commit becomes impossible rather than handled.
export const MAX_AUTOMOD_TERMS = 200;
// Matches the loyalty service's counter-key treatment (bare key, lower-cased).
const MAX_COUNTER_NAME_LEN = 64;

const MAX_COMMAND_NAME_LEN = 64;
const MAX_COMMAND_ALIASES = 25;

// Go strconv.Quote equivalent for diagnostic prose (ASCII corpus only; control
// characters fall back to JSON escaping — no fixture relies on them).
function q(s: string): string {
  return JSON.stringify(s);
}

function errDiag(itemIndex: number, code: string, message: string): ImportDiagnostic {
  return { severity: 'error', item_index: itemIndex, code, message };
}

export function warnDiag(itemIndex: number, code: string, message: string): ImportDiagnostic {
  return { severity: 'warn', item_index: itemIndex, code, message };
}

// --- the three Go validators this layer's messages lean on -------------------

// commandNameProblem mirrors validate.CommandName's error strings: 1-64 bytes
// of printable ASCII without spaces. Returns null when valid.
function commandNameProblem(name: string): string | null {
  const n = byteLen(name);
  if (n === 0 || n > MAX_COMMAND_NAME_LEN)
    return 'command name must be 1-64 printable ASCII characters without spaces';
  for (let i = 0; i < name.length; i++) {
    const c = name.charCodeAt(i);
    // Printable ASCII without space: blocks control characters, whitespace
    // tricks and invisible unicode in command lookups.
    if (c <= 0x20 || c > 0x7e)
      return 'command name must be 1-64 printable ASCII characters without spaces';
  }
  return null;
}

// commandAliasesProblem mirrors validate.CommandAliases: each alias a valid
// command name, unique case-insensitively, at most 25.
function commandAliasesProblem(aliases: string[]): string | null {
  if (aliases.length > MAX_COMMAND_ALIASES)
    return 'aliases must each be a valid command name, unique, and at most 25 in total';
  const seen = new Set<string>();
  for (const alias of aliases) {
    const problem = commandNameProblem(alias);
    if (problem) return 'aliases must each be a valid command name, unique, and at most 25 in total';
    const key = alias.toLowerCase();
    if (seen.has(key)) return 'aliases must each be a valid command name, unique, and at most 25 in total';
    seen.add(key);
  }
  return null;
}

// commandResponseProblem mirrors validate.CommandResponse: 1-5 lines, each
// 1-500 bytes without control characters.
function commandResponseProblem(response: string): string | null {
  if (byteLen(response) === 0) return 'command response must be 1-5 lines, each 1-500 characters without control characters';
  const lines = response.split('\n');
  if (lines.length > MAX_RESPONSE_LINES)
    return 'command response must be 1-5 lines, each 1-500 characters without control characters';
  for (const line of lines) {
    if (!validResponseLine(line))
      return 'command response must be 1-5 lines, each 1-500 characters without control characters';
  }
  return null;
}

function validResponseLine(line: string): boolean {
  const n = byteLen(line);
  if (n === 0 || n > MAX_RESPONSE_LINE_BYTES) return false;
  for (const ch of line) {
    if ((ch.codePointAt(0) ?? 0) < 0x20) return false;
  }
  return true;
}

// Validate walks a whole manifest and returns one diagnostic per problem,
// ordered commands, timers, triggers, quotes, counters, automod. Errors mark
// items commit must skip (the item cannot land as-is); warns mark values
// commit will adjust. It re-checks limits even though parsers run the
// canonicalizers themselves: callers (including the browser, on the Moobot
// path) are untrusted, and trust here is verified, not assumed.
export function validateManifest(m: ImportManifest | null | undefined): ImportDiagnostic[] {
  if (!m) return [];
  const diags: ImportDiagnostic[] = [];
  if (isEmptyStats(stats(m)) && !m.automod)
    diags.push(warnDiag(-1, CODE.manifestEmpty, 'manifest carries no items'));

  for (const walk of KIND_WALKERS) diags.push(...walk(m));

  if (m.automod) diags.push(...automodDiags(m.automod));
  return diags;
}

// CollectionKind is one manifest collection's validation rule: an accessor,
// its cap and the pure item validator producing that collection's diagnostics.
interface CollectionKind<T> {
  noun: string;
  cap: number;
  overflowCode: string;
  items: (m: ImportManifest) => T[];
  validateItem: (item: T, index: number) => ImportDiagnostic[];
}

// walkKind binds a kind into the walker table below. Overflow keeps the
// item's slot but skips its checks, exactly like the previous ladders.
function walkKind<T>(kind: CollectionKind<T>): (m: ImportManifest) => ImportDiagnostic[] {
  return (m) =>
    kind.items(m).flatMap((item, i) =>
      i >= kind.cap ? [errDiag(i, kind.overflowCode, `only the first ${kind.cap} ${kind.noun} are imported`)] : kind.validateItem(item, i)
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
  }),
  walkKind({
    noun: 'counters',
    cap: MAX_IMPORT_COUNTERS,
    overflowCode: CODE.counterTooMany,
    items: (m) => m.counters ?? [],
    validateItem: validateCounterItem
  })
];

function automodDiags(terms: NonNullable<ImportManifest['automod']>): ImportDiagnostic[] {
  if ((terms.block?.length ?? 0) <= MAX_AUTOMOD_TERMS && (terms.allow?.length ?? 0) <= MAX_AUTOMOD_TERMS) return [];
  return [
    warnDiag(-1, CODE.automodTermsTooMany, `automod term lists truncated to ${MAX_AUTOMOD_TERMS} entries per list at commit`)
  ];
}

// --- item validators (one per manifest collection, all pure) -----------------

function validateCommandItem(c: ManifestCommand, index: number): ImportDiagnostic[] {
  const name = normalizeName(c.name);
  return [
    ...commandNameDiags(c, name, index),
    ...commandAliasDiags(c, name, index),
    ...commandResponseDiags(c, name, index),
    ...commandTierDiags(c, index)
  ];
}

function commandNameDiags(c: ManifestCommand, name: string, index: number): ImportDiagnostic[] {
  const problem = commandNameProblem(name);
  return problem ? [errDiag(index, CODE.nameInvalid, `command name ${q(c.name)}: ${problem}`)] : [];
}

function commandAliasDiags(c: ManifestCommand, name: string, index: number): ImportDiagnostic[] {
  if (!c.aliases?.length) return [];
  const problem = commandAliasesProblem(c.aliases.map(normalizeName));
  return problem ? [errDiag(index, CODE.aliasInvalid, `aliases for ${q(name)}: ${problem}`)] : [];
}

function commandResponseDiags(c: ManifestCommand, name: string, index: number): ImportDiagnostic[] {
  if (!c.responses?.length) return [errDiag(index, CODE.responseInvalid, 'command has no response')];
  const problem = commandResponseProblem(c.responses.join('\n'));
  return problem ? [errDiag(index, CODE.responseInvalid, `response for ${q(name)}: ${problem}`)] : [];
}

function commandTierDiags(c: ManifestCommand, index: number): ImportDiagnostic[] {
  const out: ImportDiagnostic[] = [];
  if (c.permission && !PERM_TIERS.includes(c.permission)) {
    out.push(
      errDiag(
        index,
        CODE.permissionUnmapped,
        `permission ${q(c.permission)} is not one of everyone/sub/vip/mod/lead_mod/broadcaster`
      )
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
  if (t.message.trim() === '') out.push(errDiag(index, CODE.timerMessageEmpty, 'timer has no message'));
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
    errDiag(index, CODE.triggerInvalid, `trigger needs both a phrase and a response; got phrase=${q(tr.phrase)}`)
  ];
}

function validateQuoteItem(qt: ManifestQuote, index: number): ImportDiagnostic[] {
  const out: ImportDiagnostic[] = [];
  const text = qt.text.trim();
  if (text === '' || byteLen(text) > MAX_QUOTE_TEXT_LEN) {
    out.push(errDiag(index, CODE.quoteTextInvalid, `quote text must be 1-${MAX_QUOTE_TEXT_LEN} bytes`));
  }
  if (qt.created_at && !isRFC3339(qt.created_at)) {
    out.push(errDiag(index, CODE.quoteDateInvalid, 'created_at must be RFC 3339 (e.g. 2026-01-31T12:00:00Z)'));
  }
  return out;
}

function validateCounterItem(c: ManifestCounter, index: number): ImportDiagnostic[] {
  const name = normalizeName(c.name);
  if (name !== '' && byteLen(name) <= MAX_COUNTER_NAME_LEN) return [];
  return [errDiag(index, CODE.counterNameInvalid, `counter name must be 1-${MAX_COUNTER_NAME_LEN} characters`)];
}

// isRFC3339 mirrors Go time.Parse(time.RFC3339, s): strict calendar shape with
// a mandatory zone offset (Z or ±hh:mm). JS Date() accepts far too much to
// reuse here.
export function isRFC3339(s: string): boolean {
  const m = /^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})(\.\d+)?(?:[Zz]|[+-]\d{2}:\d{2})$/.exec(s);
  if (!m) return false;
  return validCalendarDay(+m[1], +m[2], +m[3]) && validClock(+m[4], +m[5], +m[6]);
}

function validCalendarDay(y: number, mo: number, d: number): boolean {
  if (mo < 1 || mo > 12 || d < 1) return false;
  const daysInMonth = [31, isLeapYear(y) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return d <= daysInMonth[mo - 1];
}

function isLeapYear(y: number): boolean {
  return (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0;
}

function validClock(h: number, mi: number, s: number): boolean {
  // s = 60 = leap second, which Go's RFC3339 parse also accepts
  return h <= 23 && mi <= 59 && s <= 60;
}

// --- failed-item lookup (commit's drop filter) -------------------------------

// FailedItems indexes a diagnostic slice into a lookup the commit path uses to
// drop unappliable items: errorItems(diags).has('commands', 3) answers whether
// manifest.Commands[3] carried an error-severity finding. Diagnostics whose
// code carries no recognized kind prefix (manifest-level findings) are skipped,
// because dropping whole collections over a global warning would turn one bad
// automod list into a silently empty import.
export class FailedItems {
  private readonly sets: Map<string, Set<number>>;

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

  has(collection: string, idx: number): boolean {
    return this.sets.get(collection)?.has(idx) ?? false;
  }
}

// failedCollection maps a diagnostic code's kind prefix onto the manifest
// collection it addresses.
const FAILED_PREFIXES: readonly [prefix: string, collection: string][] = [
  ['command', 'commands'],
  ['timer', 'timers'],
  ['trigger', 'triggers'],
  ['quote', 'quotes'],
  ['counter', 'counters']
];

function failedCollection(code: string): string | null {
  const hit = FAILED_PREFIXES.find(([prefix]) => code.startsWith(prefix));
  return hit ? hit[1] : null;
}
