// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Parse layer of the Nightbot config-import source: envelope rows in, canonical
// ImportManifest out. Envelope failures throw NightbotExportError (the wizard
// renders it as a parse failure); everything inside the envelope degrades per
// row, so one unusable command never costs the broadcaster the other fifty.
//
// The file the broadcaster brings is decoded and translated IN THE BROWSER like
// the Moobot path, and only the resulting manifest is POSTed, so the raw export
// never crosses the wire.
//
// No Go parser ever existed for this source, so unlike moobot/ and
// streamelements.ts there is no port-parity golden to reconcile against; the
// expectations in nightbot.test.ts ARE the contract.

import {
  CODE,
  MAX_AUTOMOD_TERMS,
  canonicalizeResponse,
  clampCooldown,
  mapPermission,
  normalizeName,
  warnDiag
} from '../validate';
import type {
  ImportDiagnostic,
  ImportManifest,
  ManifestCommand,
  ManifestFetch,
  ManifestTimer
} from '../types';
import { asNum, asStr, decodeEnvelope, looksLikeCommand, looksLikeTimer } from './envelope';
import type { NbRow } from './envelope';
import { makeFetchSlotSink } from './fetchdefs';
import { translateVariables } from './variables';

// Codes this parser emits beyond the shared CODE table. The *_skipped family
// marks source rows deliberately left out of the manifest: they own no manifest
// slot, so they are attributed to index -1 and name the offender instead.
export const NB_CODE = {
  commandUnparseable: 'command_unparseable_skipped',
  commandNameInvalid: 'command_name_invalid',
  commandRegularWidened: 'command_regular_widened',
  timerUnparseable: 'timer_unparseable_skipped',
  timerDisabledSkipped: 'timer_disabled_skipped',
  timerLinesIgnored: 'timer_line_gate_ignored',
  timerVariableUnmapped: 'timer_variable_unmapped',
  timerMessageTruncated: 'timer_message_truncated',
  timerLineDropped: 'timer_message_line_dropped',
  automodRegexSkipped: 'automod_term_regex_skipped',
  automodTermsCapped: 'automod_terms_capped'
} as const;

const q = (s: string): string => JSON.stringify(s);

const errAt = (itemIndex: number, code: string, message: string): ImportDiagnostic => ({
  severity: 'error',
  item_index: itemIndex,
  code,
  message
});

// State is what every row-level function needs and nothing more: the shared
// diagnostics stream and the import-level definition map. Passing it as one
// value keeps the row functions at two arguments instead of four, and keeps a
// row from ever being handed a diagnostics list that is not the real one.
class State {
  readonly diags: ImportDiagnostic[] = [];
  readonly fetchDefs = new Map<string, ManifestFetch>();

  skip(code: string, message: string): void {
    this.diags.push(warnDiag(-1, code, message));
  }
}

// Notes is one item's warning sink: every note reaches the diagnostics stream
// AND the item's own `warnings`, which the review screen renders inline, so the
// two can never drift apart by a forgotten push.
class Notes {
  private readonly kept: string[] = [];

  constructor(
    readonly state: State,
    readonly index: number
  ) {}

  add(code: string, message: string): void {
    this.state.diags.push(warnDiag(this.index, code, message));
    this.kept.push(message);
  }

  list(): string[] {
    return this.kept;
  }
}

// parseNightbot translates one saved export into a manifest plus diagnostics.
export function parseNightbot(bytes: Uint8Array): {
  manifest: ImportManifest;
  diagnostics: ImportDiagnostic[];
} {
  const env = decodeEnvelope(bytes);
  const state = new State();

  const commands = collect(env.commands, state, parseCommandRow);
  const timers = collect(env.timers, state, parseTimerRow);
  const block = parseBlacklist(env.blacklist, state);

  const manifest: ImportManifest = {};
  if (commands.length > 0) manifest.commands = commands;
  if (state.fetchDefs.size > 0) manifest.fetches = [...state.fetchDefs.values()];
  if (timers.length > 0) manifest.timers = timers;
  if (block.length > 0) manifest.automod = { block };
  return { manifest, diagnostics: state.diags };
}

// collect walks one source collection through its row parser, dropping the rows
// that own no manifest slot. The index a row parser sees is the index its item
// will have IN THE MANIFEST, which is what diagnostics address.
function collect<T>(
  rows: NbRow[],
  state: State,
  parseRow: (row: NbRow, notes: Notes) => T | null
): T[] {
  const out: T[] = [];
  for (const row of rows) {
    const item = parseRow(row, new Notes(state, out.length));
    if (item !== null) out.push(item);
  }
  return out;
}

// --- commands ----------------------------------------------------------------

// NbCommand is one exported command read into the fields this parser uses,
// so the mapping steps below take a command rather than a bag of loose strings
// pulled out of an untyped row at each call site.
interface NbCommand {
  name: string;
  message: string;
  level: string;
  cooldown: number;
}

// readCommand lifts one row into an NbCommand, or returns null when the row
// owns no manifest slot at all (unusable shape, empty name).
function readCommand(row: NbRow, notes: Notes): NbCommand | null {
  if (!looksLikeCommand(row)) {
    notes.state.skip(NB_CODE.commandUnparseable, 'skipped one Nightbot row that carries no name/message pair');
    return null;
  }
  const raw = asStr(row.name);
  const name = normalizeName(raw);
  if (name === '') {
    notes.state.diags.push(
      errAt(-1, NB_CODE.commandNameInvalid, `command ${q(raw)} normalizes to an empty name; skipped`)
    );
    return null;
  }
  return {
    name,
    message: asStr(row.message),
    level: asStr(row.userLevel),
    cooldown: clampCooldown(Math.trunc(asNum(row.coolDown) ?? 0))
  };
}

function parseCommandRow(row: NbRow, notes: Notes): ManifestCommand | null {
  const src = readCommand(row, notes);
  if (!src) return null;

  // Fields are omitted when empty so a serialized manifest matches what the
  // other parsers emit for the same content.
  const cmd: ManifestCommand = {
    name: src.name,
    responses: commandResponses(src, notes),
    permission: commandPermission(src, notes)
  };
  if (src.cooldown > 0) cmd.cooldown_seconds = src.cooldown;
  if (notes.list().length > 0) cmd.warnings = notes.list();
  return cmd;
}

// commandPermission maps Nightbot's own labels — everyone / regular /
// subscriber / twitch_vip / moderator / owner. mapPermission owns the table;
// "regular" is Nightbot's manually-granted trust tier, which this bot has no
// equivalent for, so it widens to everyone WITH a note rather than silently.
function commandPermission(src: NbCommand, notes: Notes): ManifestCommand['permission'] {
  const { perm, recognized } = mapPermission(src.level);
  if (!recognized) {
    notes.add(
      CODE.permissionUnmapped,
      `command ${q(src.name)} requires user level ${q(src.level)}, which has no equivalent here; widened to everyone`
    );
  } else if (src.level.trim().toLowerCase() === 'regular') {
    notes.add(
      NB_CODE.commandRegularWidened,
      `command ${q(src.name)} was limited to Nightbot regulars; there is no regular tier here, so it is open to everyone (widening)`
    );
  }
  return perm;
}

// commandResponses translates one command's message into chat-ready lines,
// synthesizing urlfetch definitions on the way. An empty result carries an error
// diagnostic so commit skips the command instead of writing a mute one.
function commandResponses(src: NbCommand, notes: Notes): string[] {
  const sink = makeFetchSlotSink(src.name, notes.state.fetchDefs, notes.state.diags);
  const translated = translateVariables(src.message, sink);
  for (const tok of translated.warns) {
    notes.add(CODE.variableUnmapped, `response uses ${tok}, which has no equivalent; left as literal text`);
  }
  if (translated.jsonFetch) {
    notes.add(
      CODE.variableUnmapped,
      `command ${q(src.name)} fetched JSON and picked fields out of it with $(eval …); the imported definition returns the whole response until you set a JSON path under Commands → Fetch definitions`
    );
  }

  const { lines, diags } = canonicalizeResponse(translated.text, notes.index);
  notes.state.diags.push(...diags);
  if (lines.length === 0) notes.state.diags.push(emptyResponseDiag(src, notes.index));
  return lines;
}

function emptyResponseDiag(src: NbCommand, idx: number): ImportDiagnostic {
  return errAt(
    idx,
    CODE.responseInvalid,
    src.message.trim() === ''
      ? `command ${q(src.name)} has no response`
      : `command ${q(src.name)} has no usable response after translation`
  );
}

// --- timers ------------------------------------------------------------------

// NbTimer is one exported timer read into the fields this parser uses.
interface NbTimer {
  label: string;
  message: string;
  intervalMinutes: number;
  lineGate: number;
  enabled: boolean;
}

function readTimer(row: NbRow, notes: Notes): NbTimer | null {
  if (!looksLikeTimer(row)) {
    notes.state.skip(NB_CODE.timerUnparseable, 'skipped one Nightbot timer row that carries no message/interval pair');
    return null;
  }
  return {
    label: asStr(row.name).trim() || '(unnamed)',
    message: asStr(row.message),
    intervalMinutes: asNum(row.interval) ?? 0,
    lineGate: asNum(row.lines) ?? 0,
    // `enabled` is absent on some saved responses; absent means on, matching
    // Nightbot's own default for a timer that exists at all.
    enabled: row.enabled !== false
  };
}

function parseTimerRow(row: NbRow, notes: Notes): ManifestTimer | null {
  const src = readTimer(row, notes);
  if (!src) return null;
  if (!src.enabled) {
    notes.state.skip(NB_CODE.timerDisabledSkipped, `timer ${q(src.label)} is disabled upstream and was skipped`);
    return null;
  }

  reportLineGate(src, notes);
  const message = timerMessage(src, notes);
  if (message.trim() === '') {
    notes.state.diags.push(
      errAt(notes.index, CODE.timerMessageEmpty, `timer ${q(src.label)} has no usable message after translation`)
    );
  }

  // Decision record — interval units: Nightbot timer intervals are MINUTES (its
  // dashboard labels the field "Interval (minutes)" and enforces a 5-minute
  // floor). Multiply by 60 once here so the manifest carries seconds like every
  // consumer expects; commit clamps sub-floor values itself.
  //
  // online_only is set for every Nightbot timer: Nightbot only posts timers
  // while the channel is live, so carrying that across preserves upstream
  // behavior rather than starting to announce into an offline chat.
  return {
    message,
    interval_seconds: Math.max(0, Math.trunc(src.intervalMinutes * 60)),
    online_only: true
  };
}

// Nightbot additionally gates a timer on N chat lines having passed since its
// last run. This engine's timers are interval-only, so the gate is dropped —
// the timer posts more often on a quiet stream than it did upstream, which is a
// behavior change worth naming.
function reportLineGate(src: NbTimer, notes: Notes): void {
  if (src.lineGate <= 0) return;
  notes.state.diags.push(
    warnDiag(
      notes.index,
      NB_CODE.timerLinesIgnored,
      `timer ${q(src.label)} also waited for ${src.lineGate} chat lines between posts; timers here are interval-only, so that gate is dropped`
    )
  );
}

// timerMessage translates and canonicalizes one timer's text. Timers get no
// urlfetch sink: they carry no command name to build a deterministic slug from,
// so their urlfetch tokens take the literal+unmapped-warn path.
function timerMessage(src: NbTimer, notes: Notes): string {
  const translated = translateVariables(src.message);
  for (const tok of translated.warns) {
    notes.state.diags.push(
      warnDiag(
        notes.index,
        NB_CODE.timerVariableUnmapped,
        `timer message uses ${tok}, which has no equivalent; left as literal text`
      )
    );
  }

  const { lines, diags } = canonicalizeResponse(translated.text, notes.index);
  notes.state.diags.push(...diags.map(asTimerDiag));
  return lines.join('\n');
}

// canonicalizeResponse attributes its findings with command_-prefixed codes;
// these items are timers, so the codes are re-prefixed to keep FailedItems
// dropping the right collection.
function asTimerDiag(d: ImportDiagnostic): ImportDiagnostic {
  if (d.code === CODE.responseTruncated) return { ...d, code: NB_CODE.timerMessageTruncated };
  if (d.code === CODE.responseLineDropped) return { ...d, code: NB_CODE.timerLineDropped };
  return d;
}

// --- spam protection ---------------------------------------------------------

// parseBlacklist maps Nightbot's spam-protection blacklist onto automod block
// terms. Nightbot accepts a `~/regex/` form there; this bot's automod matches
// literal terms, so a pattern entry is skipped by name instead of being imported
// as a term that would only ever match the literal text "~/…/".
function parseBlacklist(terms: string[], state: State): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const raw of terms) {
    const term = raw.trim();
    if (term === '' || seen.has(term.toLowerCase())) continue;
    if (term.startsWith('~/')) {
      state.skip(
        NB_CODE.automodRegexSkipped,
        `blacklist entry ${q(term)} is a regex pattern; automod matches literal terms, so it was skipped`
      );
      continue;
    }
    seen.add(term.toLowerCase());
    out.push(term);
  }
  return capTerms(out, state);
}

function capTerms(terms: string[], state: State): string[] {
  if (terms.length <= MAX_AUTOMOD_TERMS) return terms;
  state.skip(
    NB_CODE.automodTermsCapped,
    `${terms.length - MAX_AUTOMOD_TERMS} blacklist terms dropped past the ${MAX_AUTOMOD_TERMS}-term limit`
  );
  return terms.slice(0, MAX_AUTOMOD_TERMS);
}
