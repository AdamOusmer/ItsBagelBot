// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Envelope decoding, section parsers and the command/timer/alias expansion
// pipeline — the port of moobot.go. Tag translation lives in ./tags.
//
// Browser-side parser for Moobot settings exports (Tools -> Import & Export ->
// "Export this dashboard to a file"), ported one-for-one from
// app/importer/source/moobot (moobot.go + tags.go) plus the mapping rules that
// parser calls (NormalizeName, CanonicalizeResponse, ClampCooldown, permission
// tiers). The point of the port: the wizard decodes/validates the export IN
// THE BROWSER and POSTs only the resulting ImportManifest, so the raw file
// never crosses the wire.
//
// Parity contract: this port is pinned against the Go parser's actual output
// by moobot.test.ts, which replays the SAME fixture corpus through both
// implementations (golden regenerated deliberately via
// IMPORTER_MOOBOT_DUMP_JSON, see app/importer/source/moobot/dumpgolden_test.go
// and testdata/moobot-golden.json). When you change the Go parser, regenerate
// that golden and reconcile this file in the same change.
//
// Deliberate divergences from Go (none observable in manifests):
//  - Diagnostic MESSAGE prose mirrors the server's wording but is not
//    byte-identical (Go %v/%q formatting vs JS String()); only
//    severity/item_index/code are pinned.
//  - No worker/timeout around parse: JSON.parse is linear-time over an input
//    already hard-capped at 10MB by the caller (~150ms worst case measured),
//    and CSP forbids blob: workers here (script-src 'self'), so a worker buys
//    isolation theater, not a bound. The try/catch + size cap ARE the bound.

import type {
  ImportDiagnostic,
  ImportManifest,
  ManifestCommand,
  ManifestCounter,
  ManifestFetch,
  ManifestTimer
} from '../types';

import { translateTags } from './tags';
import type { TagContext, TextOption } from './tags';

// Codes restated from internal/domain/rpc/importer/importer.go — keep in step.
// Every code this parser emits is a row here, so call sites never repeat raw
// strings (the golden fixtures compare them verbatim).
const CODE = {
  moduleReadFailed: 'module_read_failed',
  nameInvalid: 'command_name_invalid',
  aliasInvalid: 'command_alias_invalid',
  permissionUnmapped: 'command_permission_unmapped',
  permissionWidened: 'command_permission_widened',
  permissionCollapsed: 'command_permission_collapsed',
  variableUnmapped: 'command_variable_unmapped',
  fetchUrlAbsent: 'command_fetch_url_absent',
  responseTruncated: 'command_response_truncated',
  responseLineDropped: 'command_response_line_dropped',
  intervalClamped: 'timer_interval_clamped',
  timerMessageEmpty: 'timer_message_empty',
  counterValueAbsent: 'counter_value_absent',
  counterValueFractional: 'counter_value_fractional',
  commandDisabled: 'command_disabled',
  permissionGroupsSkipped: 'permission_groups_skipped',
  responseOverridesSkipped: 'response_overrides_skipped',
  timerDisabled: 'timer_disabled',
  timerCommandUnresolved: 'timer_command_unresolved',
  aliasUnresolved: 'command_alias_unresolved',
  aliasArguments: 'command_alias_arguments'
} as const;

export const MOOBOT_SECTIONS = {
  commands: 'commands_custom',
  aliases: 'command_aliases',
  timers: 'command_timers',
  permGroups: 'permission_groups',
  respOverrides: 'responses'
} as const;

// Envelope rules mirror what Moobot's own reader enforces
// (moobot.modal.settings-import-export.js): version 1, type "settings".
const ENVELOPE_VERSION = 1;
const ENVELOPE_TYPE = 'settings';

export class MoobotExportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'MoobotExportError';
  }
}

const encoder = new TextEncoder();
const byteLen = (s: string): number => encoder.encode(s).length;

// maxResponseLineLength / maxResponseLines / maxCooldownSeconds restate
// app/importer/mapping (response.go, mapping.go): Twitch drops chat lines over
// 500 bytes; responses cap at 5 lines; cooldowns fold onto 0..86400.
const MAX_RESPONSE_LINE_BYTES = 500;
const MAX_RESPONSE_LINES = 5;
const MAX_COOLDOWN_SECONDS = 86400;

function q(s: string): string {
  return JSON.stringify(s);
}

// warnDiag/errDiag are the shared diagnostic factories; every finding in this
// parser flows through one of them so the shape lives in exactly one place.
function warnDiag(item_index: number, code: string, message: string): ImportDiagnostic {
  return { severity: 'warn', item_index, code, message };
}

function errDiag(item_index: number, code: string, message: string): ImportDiagnostic {
  return { severity: 'error', item_index, code, message };
}

// --- canonicalization primitives (ported from app/importer/mapping) ---------

// NormalizeName: trim, strip ONE leading "!", trim, lowercase — identical to
// mapping.NormalizeName so browser-produced names collide-detect the same way
// server-side.
export function normalizeName(name: string): string {
  return name.trim().replace(/^!/, '').trim().toLowerCase();
}

// truncateLineBytes cuts to at most limit BYTES without splitting a UTF-8
// sequence (the bot posts these lines verbatim; a split emote would be invalid
// UTF-8 on the wire).
function truncateLineBytes(line: string, limit: number): string {
  let bytes = 0;
  let out = '';
  for (const ch of line) {
    const n = byteLen(ch);
    if (bytes + n > limit) break;
    bytes += n;
    out += ch;
  }
  return out;
}

// canonicalizeResponse splits one source response into chat-ready lines,
// mirroring mapping.CanonicalizeResponse: CRLF folded, blank lines dropped,
// each line capped at 500 bytes, total capped at 5 lines, every lossy fix
// reported as a warn diagnostic on itemIndex.
function canonicalizeResponse(raw: string, itemIndex: number): {
  lines: string[];
  diags: ImportDiagnostic[];
} {
  const diags: ImportDiagnostic[] = [];
  const lines: string[] = [];
  for (const piece of raw.replace(/\r\n/g, '\n').replace(/\r/g, '\n').split('\n')) {
    let line = piece.trim();
    if (line === '') continue;
    if (byteLen(line) > MAX_RESPONSE_LINE_BYTES) {
      const cut = truncateLineBytes(line, MAX_RESPONSE_LINE_BYTES);
      diags.push(warnDiag(itemIndex, CODE.responseTruncated,
        `response line cut from ${byteLen(line)} to ${byteLen(cut)} bytes (Twitch per-message limit)`));
      line = cut;
    }
    lines.push(line);
  }
  if (lines.length > MAX_RESPONSE_LINES) {
    const extra = lines.length - MAX_RESPONSE_LINES;
    diags.push(warnDiag(itemIndex, CODE.responseLineDropped,
      `${extra} response line(s) dropped past the ${MAX_RESPONSE_LINES}-line limit`));
    lines.length = MAX_RESPONSE_LINES;
  }
  return { lines, diags };
}

function clampCooldown(seconds: number): number {
  if (seconds <= 0) return 0;
  return Math.min(seconds, MAX_COOLDOWN_SECONDS);
}

// --- permissions (ported from moobot.go resolveTriggerGroups) ---------------

const PERM_RANK = ['everyone', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'];

// The five builtin Moobot usergroup ids, confirmed from their command-edit
// modal; custom groups never appear in trigger_usergroups. Same table as
// moobot.go usergroupLabels.
// The five builtin Moobot usergroup ids; unknown ids are hostile-input
// tolerance and degrade through the Partial lookup below.
type MoobotUsergroup = 0 | 1 | 2 | 3 | 4;

const USERGROUP_LABELS: Partial<Record<MoobotUsergroup, string>> = {
  0: 'normal users',
  1: 'moderators',
  2: 'editors',
  3: 'regulars',
  4: 'subscribers'
};

// Shared permission alias table entries actually reachable from Moobot
// usergroup labels (subset of mapping/permission.go; the full table also
// covers other bots' spellings this parser never feeds it).
const PERMISSION_ALIASES: Record<string, string> = {
  everyone: 'everyone',
  viewers: 'everyone',
  moderators: 'mod',
  editors: 'mod',
  regulars: 'everyone',
  subscribers: 'sub'
};

function mapPermission(label: string): { perm: string; recognized: boolean } {
  const key = label.trim().toLowerCase();
  if (key === '') return { perm: 'everyone', recognized: true };
  const mapped = PERMISSION_ALIASES[key];
  if (mapped) return { perm: mapped, recognized: true };
  return { perm: 'everyone', recognized: false };
}

function widestTier(tiers: string[]): string {
  let best = PERM_RANK[0];
  let bestIdx = PERM_RANK.length;
  for (const tier of tiers) {
    const i = PERM_RANK.indexOf(tier);
    if (i >= 0 && i < bestIdx) {
      bestIdx = i;
      best = tier;
    }
  }
  return best;
}

interface PermResult {
  perm: string;
  diags: ImportDiagnostic[];
}

function resolveTriggerGroups(ids: number[], itemIndex: number): PermResult {
  if (ids.length === 0) return { perm: '', diags: [] };
  const diags: ImportDiagnostic[] = [];
  const distinct: string[] = [];
  const seen = new Set<string>();
  const labels: string[] = [];
  for (const id of ids) {
    const resolved = resolveOneGroup(id, itemIndex, diags);
    labels.push(resolved.label);
    if (!seen.has(resolved.perm)) {
      seen.add(resolved.perm);
      distinct.push(resolved.perm);
    }
  }
  const perm = widestTier(distinct);
  if (distinct.length > 1) {
    diags.push(warnDiag(itemIndex, CODE.permissionCollapsed,
      `allowed groups [${labels.join(', ')}] collapse to the widest tier ${q(perm)}; the narrower restrictions are lost`));
  }
  return { perm, diags };
}

// resolveOneGroup maps one builtin usergroup id to its tier, emitting that
// id's diagnostics in order: unknown id, unrecognized feed, widened regulars.
function resolveOneGroup(id: number, itemIndex: number, diags: ImportDiagnostic[]): { perm: string; label: string } {
  let label = USERGROUP_LABELS[id as MoobotUsergroup];
  if (label === undefined) {
    diags.push(warnDiag(itemIndex, CODE.permissionUnmapped, `unknown user group id ${id} treated as everyone`));
    label = 'everyone';
  }
  // id 0 ("normal users") feeds the shared table as everyone; id 2
  // (editors) narrows to moderators — Moobot editors outrank mods there,
  // but our ladder has no editor tier and widening would invent trust
  // (same decision record as moobot.go).
  const feed = id === 0 ? 'everyone' : id === 2 ? 'moderators' : label;
  const { perm, recognized } = mapPermission(feed);
  if (!recognized) {
    diags.push(warnDiag(itemIndex, CODE.permissionUnmapped, `permission group ${q(label)} is not recognized; defaulted to everyone`));
  }
  if (id === 3) {
    // Regulars widens to everyone: we have no regular tier (CONTRACT §7).
    diags.push(warnDiag(itemIndex, CODE.permissionWidened,
      'Moobot regulars widen to everyone here (no regular tier); any viewer may run it'));
  }
  return { perm, label };
}


// --- raw shape --------------------------------------------------------------

interface RawSection {
  type?: unknown;
  data?: unknown;
}

interface RawDocument {
  version?: unknown;
  type?: unknown;
  settings?: unknown;
}

interface RawCommand {
  identifier?: unknown;
  text?: unknown;
  enabled?: unknown;
  cooldown?: unknown;
  trigger_usergroups?: unknown;
  counter?: unknown;
  random_number_range_start?: unknown;
  random_number_range_end?: unknown;
  random_text_1?: unknown;
  random_text_2?: unknown;
  random_text_3?: unknown;
}

interface RawAlias {
  alias?: unknown;
  id?: unknown;
  type?: unknown;
  arguments?: unknown;
}

interface RawTimer {
  description?: unknown;
  enabled?: unknown;
  time?: unknown;
  commands?: unknown;
}

const asStr = (v: unknown): string => (typeof v === 'string' ? v : '');
const asNum = (v: unknown): number | undefined => (typeof v === 'number' && Number.isFinite(v) ? v : undefined);
const asObjArray = (v: unknown): Record<string, unknown>[] =>
  Array.isArray(v) ? v.filter((x): x is Record<string, unknown> => x !== null && typeof x === 'object' && !Array.isArray(x)) : [];

// stripBOM removes the UTF-8 byte-order mark Moobot exports can carry (Go's
// decoder tolerates it mid-stream; JSON.parse does not, so drop it first).
function stripBom(bytes: Uint8Array): Uint8Array {
  return bytes.length >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf
    ? bytes.subarray(3)
    : bytes;
}

function decodeJson(bytes: Uint8Array): unknown {
  // fatal:false matches Go's decoder, which replaces invalid UTF-8 rather
  // than failing on it; syntax errors still throw like json.Unmarshal.
  const text = new TextDecoder('utf-8', { fatal: false }).decode(stripBom(bytes));
  return JSON.parse(text);
}

function decodeEnvelope(bytes: Uint8Array): Record<string, unknown>[] {
  let doc: RawDocument;
  try {
    doc = decodeJson(bytes) as RawDocument;
  } catch (err) {
    throw new MoobotExportError(`importer/moobot: not a JSON export file: ${(err as Error)?.message ?? String(err)}`);
  }
  if (typeof doc !== 'object' || doc === null || Array.isArray(doc)) {
    throw new MoobotExportError('importer/moobot: not a JSON export file: json: cannot unmarshal into envelope');
  }
  if (typeof doc.version !== 'number' || doc.version !== ENVELOPE_VERSION) {
    const v = typeof doc.version === 'number' ? String(doc.version) : '<absent>';
    throw new MoobotExportError(`importer/moobot: unsupported export version ${v} (want ${ENVELOPE_VERSION})`);
  }  if (doc.type !== ENVELOPE_TYPE) {
    throw new MoobotExportError(`importer/moobot: not a settings export (type ${q(asStr(doc.type))})`);
  }
  if (!Array.isArray(doc.settings)) {
    throw new MoobotExportError('importer/moobot: export carries no settings array');
  }
  return doc.settings.filter(
    (s): s is Record<string, unknown> => s !== null && typeof s === 'object' && !Array.isArray(s)
  );
}

export function detectMoobot(bytes: Uint8Array): boolean {
  let sections: Record<string, unknown>[];
  try {
    sections = decodeEnvelope(bytes);
  } catch {
    return false;
  }
  if (sections.length === 0) return false;
  return sections.some((s) => {
    const t = asStr(s.type);
    return (
      t === MOOBOT_SECTIONS.commands ||
      t === MOOBOT_SECTIONS.aliases ||
      t === MOOBOT_SECTIONS.timers ||
      t === MOOBOT_SECTIONS.permGroups ||
      t === MOOBOT_SECTIONS.respOverrides
    );
  });
}

function sectionDiag(secType: string, err: unknown): ImportDiagnostic {
  return warnDiag(-1, CODE.moduleReadFailed,
    `section ${q(secType)} could not be decoded (${(err as Error)?.message ?? String(err)}); skipped`);
}

// ParseState threads one export's accumulators through the per-section
// parsers. Sections may arrive in any order, so aliases/timers stage until
// after the command pass (as in moobot.go).
interface ParseState {
  commands: ManifestCommand[];
  counters: ManifestCounter[];
  timers: ManifestTimer[];
  diags: ImportDiagnostic[];
  // texts accumulates each translated response by normalized identifier so
  // timers can expand against it.
  texts: Map<string, string>;
  stagedAliases: RawAlias[];
  stagedTimers: RawTimer[];
  // fetchDefs accumulates synthesized urlfetch definition shells, deduped by
  // slug across the whole export; emitted as manifest.fetches after the walk.
  fetchDefs: Map<string, ManifestFetch>;
}

// SectionParser handles one decoded settings section's object rows. Parsers
// needing the RAW payload length read it off sec (the responses counter counts
// non-object entries too); everyone else uses items.
type SectionParser = (items: Record<string, unknown>[], sec: RawSection, state: ParseState) => void;

// decodeSection mirrors Go's []commandItem unmarshal: a non-array payload is a
// decode failure, and non-object entries drop from the rows.
function decodeSection(sec: RawSection): Record<string, unknown>[] {
  if (!Array.isArray(sec.data)) throw new Error('data is not an array');
  return asObjArray(sec.data);
}

// guarded degrades one section's decode failure into a module_read_failed
// diagnostic so the remaining sections still import.
function guarded(parse: SectionParser): (sec: RawSection, state: ParseState) => void {
  return (sec, state) => {
    try {
      parse(decodeSection(sec), sec, state);
    } catch (err) {
      state.diags.push(sectionDiag(asStr(sec.type), err));
    }
  };
}

const SECTION_PARSERS: Record<string, (sec: RawSection, state: ParseState) => void> = {
  [MOOBOT_SECTIONS.commands]: guarded(commandsSection),
  [MOOBOT_SECTIONS.aliases]: guarded(aliasSection),
  [MOOBOT_SECTIONS.timers]: guarded(timerSection),
  [MOOBOT_SECTIONS.permGroups]: guarded(permissionGroupSection),
  [MOOBOT_SECTIONS.respOverrides]: guarded(responseOverrideSection)
};

function aliasSection(items: Record<string, unknown>[], _sec: RawSection, state: ParseState): void {
  state.stagedAliases.push(...(items as RawAlias[]));
}

function timerSection(items: Record<string, unknown>[], _sec: RawSection, state: ParseState): void {
  state.stagedTimers.push(...(items as RawTimer[]));
}

function permissionGroupSection(items: Record<string, unknown>[], _sec: RawSection, state: ParseState): void {
  const names = items.map((g) => asStr(g.name)).filter((n) => n !== '');
  state.diags.push(warnDiag(-1, CODE.permissionGroupsSkipped,
    `${names.length} custom permission group(s) [${names.join(', ')}] gate dashboard access there and have no equivalent; skipped`));
}

function responseOverrideSection(_items: Record<string, unknown>[], sec: RawSection, state: ParseState): void {
  state.diags.push(warnDiag(-1, CODE.responseOverridesSkipped,
    `${(sec.data as unknown[]).length} customized built-in response template(s) have no importable target; skipped`));
}

// parseMoobot translates one export file into a manifest plus diagnostics,
// mirroring moobot.go Parse. Envelope failures throw MoobotExportError
// (handler-level parse_failed); everything inside the envelope degrades
// per-item so the good commands still import.
export function parseMoobot(bytes: Uint8Array): {
  manifest: ImportManifest;
  diagnostics: ImportDiagnostic[];
} {
  const sections = decodeEnvelope(bytes);
  const state: ParseState = {
    commands: [],
    counters: [],
    timers: [],
    diags: [],
    texts: new Map(),
    stagedAliases: [],
    stagedTimers: [],
    fetchDefs: new Map()
  };

  for (const sec of sections) SECTION_PARSERS[asStr(sec.type)]?.(sec, state);

  applyAliases(state);
  applyTimers(state);

  const manifest: ImportManifest = {};
  if (state.commands.length) manifest.commands = state.commands;
  if (state.timers.length) manifest.timers = state.timers;
  if (state.counters.length) manifest.counters = state.counters;
  if (state.fetchDefs.size > 0) manifest.fetches = [...state.fetchDefs.values()];

  return { manifest, diagnostics: state.diags };
}

function commandsSection(items: Record<string, unknown>[], _sec: RawSection, state: ParseState): void {
  items.forEach((raw, pos) => parseCommandItem(raw as RawCommand, pos, state));
}

// parseCommandItem translates one custom command. Diagnostic order below is
// pinned by the golden fixtures: permission findings, tag warnings, response
// canonicalization, counter notes, then the disabled marker.
function parseCommandItem(item: RawCommand, pos: number, state: ParseState): void {
  const name = normalizeName(asStr(item.identifier));
  if (name === '') {
    state.diags.push(errDiag(-1, CODE.nameInvalid,
      `commands_custom entry ${pos} has an empty name; skipped`));
    return;
  }

  const idx = state.commands.length;
  const cmd: ManifestCommand = { name };

  const { perm, diags: permDiags } = resolveTriggerGroups(numOfList(item.trigger_usergroups), idx);
  if (perm !== '') cmd.permission = perm as ManifestCommand['permission'];
  state.diags.push(...permDiags);

  applyCommandCooldown(item, cmd);

  const ctx: TagContext = commandTagContext(item, name, state.fetchDefs);
  const tr = translateTags(asStr(item.text), ctx);
  for (const tok of tr.unmapped) {
    state.diags.push(warnDiag(idx, CODE.variableUnmapped,
      `response uses <${tok}>, which has no equivalent; left as literal text`));
  }
  for (const ref of tr.fetchRefs) {
    // One targeted warn per distinct tag per command, in the shape of the
    // variableUnmapped precedent: the tag itself mapped fine, but its
    // definition is a URL-less shell until the broadcaster acts.
    state.diags.push(warnDiag(idx, CODE.fetchUrlAbsent,
      `response uses <${ref.tag}>, imported as {urlfetch:${ref.key}} — re-enter the URL for "${ref.key}" before it can fetch`));
  }
  const { lines, diags: respDiags } = canonicalizeResponse(tr.text, idx);
  if (lines.length) cmd.responses = lines;
  state.diags.push(...respDiags);

  applyCounterValue(item, name, tr.counterUsed, state);

  if (item.enabled === false) {
    // Kept with an error rather than dropped: preview shows exactly
    // why it cannot land while commit skips it.
    state.diags.push(errDiag(idx, CODE.commandDisabled,
      'command is disabled in Moobot; importing would enable it, so commit will skip it'));
  }

  state.commands.push(cmd);
  state.texts.set(name, tr.text);
}

function applyCommandCooldown(item: RawCommand, cmd: ManifestCommand): void {
  const cooldown = asNum(item.cooldown);
  if (cooldown === undefined) return;
  const clamped = clampCooldown(Math.trunc(cooldown));
  if (clamped > 0) cmd.cooldown_seconds = clamped;
}

function commandTagContext(item: RawCommand, name: string, fetchDefs: Map<string, ManifestFetch>): TagContext {
  return {
    name,
    fetchDefs,
    randomStart: asNum(item.random_number_range_start),
    randomEnd: asNum(item.random_number_range_end),
    randomTexts: [
      optionsOf(item.random_text_1),
      optionsOf(item.random_text_2),
      optionsOf(item.random_text_3)
    ]
  };
}

// applyCounterValue records a command's counter start value. idx is derived
// from the command pass — it equals the slot this command is about to take.
function applyCounterValue(item: RawCommand, name: string, counterUsed: boolean, state: ParseState): void {
  const idx = state.commands.length;
  if (counterUsed && asNum(item.counter) === undefined) {
    state.diags.push(warnDiag(idx, CODE.counterValueAbsent,
      `<counter> imported as {counter:${name}}; it starts at 0 because the export carries no counter value`));
  }
  const counter = asNum(item.counter);
  if (counter === undefined) return;
  const value = Math.trunc(counter);
  if (counter !== value) {
    state.diags.push(warnDiag(idx, CODE.counterValueFractional,
      `counter value ${counter} floored to ${value} (counters are whole numbers here)`));
  }
  state.counters.push({ name, value });
}

function numOfList(v: unknown): number[] {
  return Array.isArray(v) ? v.filter((x): x is number => typeof x === 'number' && Number.isFinite(x)) : [];
}

// Timer entries reference commands by IDENTIFIER STRING ("discord"); numbers
// here are hostile-input tolerance, everything else degrades to nothing.
function strList(v: unknown): string[] {
  return Array.isArray(v)
    ? v.filter((x): x is string | number => typeof x === 'string' || typeof x === 'number').map(String)
    : [];
}

function optionsOf(v: unknown): TextOption[] | undefined {
  if (!Array.isArray(v)) return undefined;
  return v
    .filter((x): x is Record<string, unknown> => x !== null && typeof x === 'object')
    .map((x) => ({ text: asStr(x.text) }));
}

// applyAliases attaches alias names onto their target custom commands; all
// alias diagnostics are manifest-level (-1), matching moobot.go.
function applyAliases(state: ParseState): void {
  const byName = new Map<string, ManifestCommand>(state.commands.map((c) => [c.name, c]));
  for (const a of state.stagedAliases) {
    attachAlias(a, byName, state.diags);
  }
}

function attachAlias(a: RawAlias, byName: Map<string, ManifestCommand>, diags: ImportDiagnostic[]): void {
  const target = byName.get(normalizeName(asStr(a.id)));
  if (asStr(a.type) !== 'custom' || !target) {
    diags.push(warnDiag(-1, CODE.aliasUnresolved,
      `alias ${q(asStr(a.alias))} targets ${asStr(a.type)} command ${q(asStr(a.id))}, which is not part of this export; skipped`));
    return;
  }
  const alias = normalizeName(asStr(a.alias));
  if (alias === '') {
    diags.push(warnDiag(-1, CODE.aliasInvalid,
      `alias of ${q(target.name)} is empty after normalization; skipped`));
    return;
  }
  if (alias === target.name || target.aliases?.includes(alias)) return;
  (target.aliases ??= []).push(alias);
  noteAliasArguments(a, diags);
}

function noteAliasArguments(a: RawAlias, diags: ImportDiagnostic[]): void {
  const args = a.arguments;
  if (typeof args === 'string' && args !== '') {
    diags.push(warnDiag(-1, CODE.aliasArguments,
      `alias ${q(asStr(a.alias))} appends fixed arguments ${q(args)}, which have no equivalent; dropped`));
  }
}

// applyTimers expands each Moobot timer into one ManifestTimer per referenced
// command present in the export; a disabled timer is dropped outright (nothing
// user-authored is lost — entries synthesize from referenced commands).
function applyTimers(state: ParseState): void {
  for (const t of state.stagedTimers) expandTimer(t, state);
}

// SECONDS_PER_MINUTE converts Moobot's minute-based timer.time column.
// Decision record (ported from moobot.go): Moobot stores intervals in MINUTES
// while the manifest carries seconds; the dashboard enforces >=1 minute, so a
// missing or nonsense time falls back to one minute rather than earning a
// clamp warning.
const SECONDS_PER_MINUTE = 60;

const MOOBOT_FALLBACK_INTERVAL_SECONDS = SECONDS_PER_MINUTE;

function timerIntervalSeconds(t: RawTimer): number {
  const time = asNum(t.time);
  return time !== undefined && time > 0 ? Math.trunc(time * SECONDS_PER_MINUTE) : MOOBOT_FALLBACK_INTERVAL_SECONDS;
}

function expandTimer(t: RawTimer, state: ParseState): void {
  let desc = asStr(t.description);
  if (desc === '') desc = '<unnamed>';
  if (t.enabled === false) {
    state.diags.push(errDiag(-1, CODE.timerDisabled,
      `timer ${q(desc)} is disabled in Moobot; importing would enable it, so it is skipped`));
    return;
  }

  expandTimerCommands(t, { desc, interval: timerIntervalSeconds(t) }, state);
}

// TimerPlan is one enabled timer's resolved identity and cadence before its
// referenced commands are expanded.
interface TimerPlan {
  desc: string;
  interval: number;
}

function expandTimerCommands(t: RawTimer, plan: TimerPlan, state: ParseState): void {
  const { desc, interval } = plan;
  const idents = strList(t.commands);
  if (idents.length === 0) {
    state.diags.push(errDiag(-1, CODE.timerMessageEmpty,
      `timer ${q(desc)} lists no commands; skipped`));
    return;
  }

  const firstIdx = state.timers.length;
  const resolution = resolveTimerCommands(idents, plan, state);
  if (!resolution.resolved) {
    state.diags.push(errDiag(-1, CODE.timerMessageEmpty,
      `timer ${q(desc)} references commands missing from this export (${resolution.unresolved.join(', ')}); skipped`));
  } else if (resolution.unresolved.length > 0) {
    state.diags.push(warnDiag(firstIdx, CODE.timerCommandUnresolved,
      `timer ${q(desc)} also references commands missing from this export (${resolution.unresolved.join(', ')})`));
  }
}

// resolveTimerCommands appends one timer entry per distinct identifier that
// resolves against the command pass's translated texts.
// resolveTimerCommands appends one timer entry per distinct identifier that
// resolves against the command pass's translated texts.
function resolveTimerCommands(
  idents: string[],
  plan: TimerPlan,
  state: ParseState
): { resolved: boolean; unresolved: string[] } {
  const unresolved: string[] = [];
  let resolved = false;
  const seen = new Set<string>();
  for (const ident of idents) {
    const name = normalizeName(ident);
    if (seen.has(name)) continue;
    seen.add(name);
    const text = state.texts.get(name);
    if (text === undefined || text.trim() === '') {
      unresolved.push(ident);
      continue;
    }
    const idx = state.timers.length;
    const { lines, diags: msgDiags } = canonicalizeResponse(text, idx);
    state.diags.push(...msgDiags);
    state.timers.push({
      message: lines.join('\n'),
      interval_seconds: plan.interval,
      online_only: true
    });
    resolved = true;
  }
  return { resolved, unresolved };
}
