// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Feature-table readers and row parsers — the port of extract.go. Commands,
// timers and quotes each get one reader over the SectionContext built by
// ./index; quote dates parse through the strict layout table below.

import type { Database } from 'sql.js';
import type { ImportDiagnostic, ImportManifest } from '../types';
import { CODE, canonicalizeResponse, clampCooldown, normalizeName } from '../validate';
import { translateVariables, mapPermissionSLCB, SLCB_CODE } from './parameters';
import { Row, selectAll, findTable, MAX_SCAN_ROWS, goAtoi, warnDiag, errDiag,
  COMMAND_TABLE_CANDIDATES, TIMER_TABLE_CANDIDATES, QUOTE_TABLE_CANDIDATES } from './dbfile';

const COMMAND_NAME_COLUMNS = ['name', 'commandname', 'command'];
const COMMAND_RESPONSE_COLUMNS = ['response', 'responsetext', 'commandresponse'];
const COMMAND_PERM_COLUMNS = ['permission', 'permissionname', 'permlvl', 'permlevel'];
const COMMAND_COOLDOWN_COLUMNS = ['cooldown', 'cooldownseconds', 'globalcooldown'];
const COMMAND_ENABLED_COLUMNS = ['enabled', 'active', 'isenabled'];
const COMMAND_TYPE_COLUMNS = ['type', 'commandtype'];

const TIMER_MESSAGE_COLUMNS = ['message', 'response', 'text'];
const TIMER_MINUTES_COLUMNS = ['interval', 'intervalminutes', 'intervalmins'];
const TIMER_SECONDS_COLUMNS = ['intervalseconds', 'intervalsec'];

const QUOTE_TEXT_COLUMNS = ['quote', 'quotetext', 'text', 'message'];
const QUOTE_DATE_COLUMNS = ['date', 'createdate', 'creationdate', 'createdat', 'datetime'];
const QUOTE_AUTHOR_COLUMNS = ['addedby', 'author', 'creator'];

// truthy lists the string spellings SLCB's persistence layer has been seen to
// store booleans with (.NET "True"/"False" among them).
const TRUTHY = new Set(['1', 'true', 'yes']);

// defaultTimerIntervalSeconds backs timers whose source rows carry no usable
// interval column. SLCB keeps the timer interval in one global settings blob
// ("the interval is completely based on the Setting at the top" per its own
// docs), which lives outside the tables this parser reads; 600s is the bot's
// long-standing UI default (10 minutes). A wrong guess lands in the dashboard
// as an editable number, whereas dropping every timer would lose them outright.
export const DEFAULT_TIMER_INTERVAL_SECONDS = 600;

export interface SectionContext {
  db: Database;
  tables: Map<string, string>;
  diags: ImportDiagnostic[];
}

// missingTableNotes degrades each absent feature table to a manifest-level
// note instead of failing the import.
export function missingTableNotes(tables: Map<string, string>): ImportDiagnostic[] {
  const sections = [
    ['commands', COMMAND_TABLE_CANDIDATES],
    ['timers', TIMER_TABLE_CANDIDATES],
    ['quotes', QUOTE_TABLE_CANDIDATES]
  ] as const;
  const notes: ImportDiagnostic[] = [];
  for (const [label, candidates] of sections) {
    if (findTable(tables, candidates) === '') {
      notes.push(manifestWarn(`no "${label}" table found in Chatbot.db; that section was skipped`));
    }
  }
  return notes;
}


export function manifestWarn(message: string): ImportDiagnostic {
  return warnDiag(-1, SLCB_CODE.manifestSourceNote, message);
}

// reindex rewrites item-level diagnostics onto the item's final manifest index
// after the name-sort, so preview highlights the right row.
function reindex(diags: ImportDiagnostic[], idx: number): void {
  for (const d of diags) d.item_index = idx;
}

// readTable selects one feature table and scans it whole; a missing table or
// read failure degrades to an empty list plus one manifest-level diagnostic —
// the shared preamble every extractor used to repeat.
function readTable(ctx: SectionContext, candidates: string[]): Row[] {
  const table = findTable(ctx.tables, candidates);
  if (table === '') return [];
  try {
    const sel = selectAll(ctx.db, table);
    if (sel.truncated) {
      ctx.diags.push(manifestWarn(
        `the "${table}" table has more than ${MAX_SCAN_ROWS} rows; only the first ${MAX_SCAN_ROWS} were imported`
      ));
    }
    return sel.rows;
  } catch (err) {
    ctx.diags.push(manifestWarn(`could not read the "${table}" table: ${String(err)}`));
    return [];
  }
}

// --- extraction ---------------------------------------------------------------

// extractCommands reads the commands table into canonical commands. Disabled
// rows are skipped (counted into one manifest-level warn); rows with neither
// name nor response are treated as junk and dropped silently.
export function extractCommands(ctx: SectionContext): NonNullable<ImportManifest['commands']> {
  const { db, tables, diags } = ctx;
  const entries: NonNullable<ImportManifest['commands']>[number][] = [];
  const allWarns: ImportDiagnostic[][] = [];
  let disabled = 0;

  for (const r of readTable(ctx, COMMAND_TABLE_CANDIDATES)) {
    if (isJunkCommandRow(r)) continue;
    if (isDisabledCommandRow(r)) {
      disabled++;
      continue;
    }
    const { cmd, warns } = buildCommandRow(r);
    entries.push(cmd);
    allWarns.push(warns);
  }

  // Stable sort by normalized name, then reindex diagnostics onto final slots.
  orderStable(entries, allWarns, (c) => normalizeName(c.name), diags);

  if (disabled > 0) {
    diags.push(manifestWarn(
      `${disabled} disabled command(s) were not imported; enable them in Streamlabs Chatbot first if wanted`
    ));
  }
  return entries;
}

function isJunkCommandRow(r: Row): boolean {
  const name = r.first(COMMAND_NAME_COLUMNS);
  return !name.present || name.value.trim() === '';
}

function isDisabledCommandRow(r: Row): boolean {
  const enabled = r.first(COMMAND_ENABLED_COLUMNS);
  return enabled.present && !TRUTHY.has(enabled.value.trim().toLowerCase());
}

// buildCommandRow translates one live command row. Canonicalization findings
// carry index 0 here; the post-sort reindex pass rewrites them onto final
// manifest slots.
function buildCommandRow(r: Row): {
  cmd: NonNullable<ImportManifest['commands']>[number];
  warns: ImportDiagnostic[];
} {
  const nameRaw = r.first(COMMAND_NAME_COLUMNS).value;
  const normName = normalizeName(nameRaw);
  const response = r.first(COMMAND_RESPONSE_COLUMNS).value;

  const res = translateVariables(response, normName);
  const canon = canonicalizeResponse(res.text, 0);

  const cmd: NonNullable<ImportManifest['commands']>[number] = {
    name: nameRaw,
    responses: canon.lines
  };
  const warns: ImportDiagnostic[] = [...res.diags, ...canon.diags];

  applyRowPermission(r, cmd, warns);
  applyRowCooldown(r, cmd);

  let external = res.external;
  if (r.valueOf(COMMAND_TYPE_COLUMNS).toLowerCase().includes('script')) external = true;
  if (external) {
    warns.push(warnDiag(
      -1,
      SLCB_CODE.scriptDependent,
      `command ${JSON.stringify(normName)} depends on a script or external API/file call; that part stays literal text until re-implemented here`
    ));
  }
  if (canon.lines.length === 0) {
    warns.push(errDiag(
      -1,
      CODE.responseInvalid,
      `command ${JSON.stringify(normName)} has no usable response after normalization`
    ));
  }
  return { cmd, warns };
}

function applyRowPermission(
  r: Row,
  cmd: NonNullable<ImportManifest['commands']>[number],
  warns: ImportDiagnostic[]
): void {
  const perm = r.first(COMMAND_PERM_COLUMNS);
  if (!perm.present) return;
  const mapped = mapPermissionSLCB(perm.value);
  cmd.permission = mapped.perm as typeof cmd.permission;
  warns.push(...mapped.diags);
}

function applyRowCooldown(r: Row, cmd: NonNullable<ImportManifest['commands']>[number]): void {
  const cd = r.first(COMMAND_COOLDOWN_COLUMNS);
  if (!cd.present) return;
  const secs = goAtoi(cd.value.trim());
  // SLCB stores command cooldowns in seconds (its chat helper
  // "!Command Cooldown <cmd> <minutes>" converts before write); the
  // minutes reading was rejected — FORMAT_NOTES.md carries why.
  // omitempty parity: a clamped 0 is omitted, like the Go manifest.
  if (secs !== null && clampCooldown(secs) > 0) cmd.cooldown_seconds = clampCooldown(secs);
}

// extractTimers reads timer messages. SLCB fires all timers off one global
// interval stored in its settings blob rather than per row, so a per-row
// interval column is used only when present; otherwise every timer carries
// DEFAULT_TIMER_INTERVAL_SECONDS with a single manifest-level warn saying so.
export function extractTimers(ctx: SectionContext): NonNullable<ImportManifest['timers']> {
  const { db, tables, diags } = ctx;
  const entries: NonNullable<ImportManifest['timers']>[number][] = [];
  const allWarns: ImportDiagnostic[][] = [];
  let defaultedInterval = false;

  for (const r of readTable(ctx, TIMER_TABLE_CANDIDATES)) {
    const t = parseTimerRow(r);
    if (!t) continue;
    defaultedInterval ||= t.defaulted;
    entries.push(t.entry);
    allWarns.push(t.warns);
  }

  orderStable(entries, allWarns, (t) => t.message, diags);

  if (defaultedInterval) {
    diags.push(manifestWarn(
      `Chatbot.db stores the timer interval globally, not per timer; every imported timer defaults to ${DEFAULT_TIMER_INTERVAL_SECONDS}s — adjust in the dashboard`
    ));
  }
  return entries;
}

// parseTimerRow translates one live timer row; null drops it silently. A row
// with no usable interval marks `defaulted` even if its message later
// collapses, matching the upstream flag's timing.
function parseTimerRow(r: Row): { entry: NonNullable<ImportManifest['timers']>[number]; warns: ImportDiagnostic[]; defaulted: boolean } | null {
  const read = r.first(TIMER_MESSAGE_COLUMNS);
  if (!read.present || read.value.trim() === '') return null;

  const message = read.value;

  const iv = timerInterval(r);
  const interval = iv.known ? iv.seconds : DEFAULT_TIMER_INTERVAL_SECONDS;

  const res = translateVariables(message, '');
  const translated = res.text.trim();
  if (translated === '') return null; // translation collapsed the message entirely

  return {
    entry: { message: translated, interval_seconds: interval },
    warns: [...res.diags],
    defaulted: !iv.known
  };
}

// timerInterval resolves one timer row's interval in confirmed units: SLCB's
// UI expresses the setting in minutes (its docs say timers post "after an
// interval of X minutes"), so minute-named columns multiply by 60 and only an
// explicit seconds-named column passes through raw.
function timerInterval(r: Row): { seconds: number; known: boolean } {
  const minutes = positiveInt(r.valueOf(TIMER_MINUTES_COLUMNS));
  if (minutes !== null) return { seconds: minutes * 60, known: true };
  const seconds = positiveInt(r.valueOf(TIMER_SECONDS_COLUMNS));
  if (seconds !== null) return { seconds, known: true };
  return { seconds: 0, known: false };
}

function positiveInt(raw: string): number | null {
  const n = goAtoi(raw.trim());
  return n !== null && n > 0 ? n : null;
}

// extractQuotes reads saved quotes. ExtraQuotes (the separate user-repurposed
// list feature) is deliberately NOT read: those entries are usually GIF/URL
// collections wired to custom commands, not quotes; importing them as quotes
// would surprise (decision recorded in FORMAT_NOTES.md).
export function extractQuotes(ctx: SectionContext): NonNullable<ImportManifest['quotes']> {
  const { db, tables, diags } = ctx;
  const entries: NonNullable<ImportManifest['quotes']>[number][] = [];
  const allWarns: ImportDiagnostic[][] = [];

  for (const r of readTable(ctx, QUOTE_TABLE_CANDIDATES)) {
    const parsed = parseQuoteRow(r);
    if (!parsed) continue;
    entries.push(parsed.entry);
    allWarns.push(parsed.warns);
  }

  orderStable(entries, allWarns, (q) => q.text, diags);
  return entries;
}

function parseQuoteRow(r: Row): { entry: NonNullable<ImportManifest['quotes']>[number]; warns: ImportDiagnostic[] } | null {
  const textRead = r.first(QUOTE_TEXT_COLUMNS);
  const text = textRead.value.trim();
  if (!textRead.present || text === '') return null;

  const q: NonNullable<ImportManifest['quotes']>[number] = { text };
  const warns: ImportDiagnostic[] = [];

  const author = r.first(QUOTE_AUTHOR_COLUMNS);
  if (author.present) q.added_by = author.value.trim();

  const date = r.first(QUOTE_DATE_COLUMNS);
  if (date.present) {
    const ts = parseQuoteDate(date.value);
    // Date format is channel-configurable in SLCB (default shown in its
    // docs is MM/DD/YYYY); an unparseable value must not block the quote
    // itself, it just loses created_at.
    if (ts !== null) q.created_at = ts;
    else warns.push(quoteDateDiag(date.value));
  }
  return { entry: q, warns };
}

function quoteDateDiag(dateRaw: string): ImportDiagnostic {
  return warnDiag(
    -1,
    SLCB_CODE.quoteDateUnparsed,
    `quote date ${JSON.stringify(dateRaw)} uses a format this importer does not know; imported without a date`
  );
}

// orderStable sorts items by key (ties keep insertion order, matching Go's
// sort.SliceStable), reindexes each item's diagnostics onto its final slot and
// appends them to the section's diagnostic stream.
function orderStable<T>(items: T[], warns: ImportDiagnostic[][], key: (item: T) => string, sink: ImportDiagnostic[]): void {
  const order = items
    .map((item, i) => ({ item, key: key(item), i }))
    .sort((a, b) => (a.key < b.key ? -1 : a.key > b.key ? 1 : a.i - b.i));
  const sortedItems: T[] = [];
  const sortedWarns: ImportDiagnostic[][] = [];
  order.forEach(({ item, i }, slot) => {
    sortedItems.push(item);
    reindex(warns[i], slot);
    sortedWarns.push(warns[i]);
  });
  items.length = 0;
  items.push(...sortedItems);
  for (const w of sortedWarns) sink.push(...w);
}

// --- dates ---------------------------------------------------------------------

// parseQuoteDate tries the layouts SLCB is known to persist dates with — its
// configurable display format plus the two storage formats .NET's SQLite layer
// emits natively — and returns RFC 3339 (UTC) on success.
export function parseQuoteDate(raw: string): string | null {
  raw = raw.trim();
  // Ordered like the Go layout table; each parser is strict about padding so
  // day-first ("31/12/2015") is rejected rather than guessed.
  const parsers: ((s: string) => DateUTC | null)[] = [
    parseRFC3339,
    exact(/^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2}):(\d{2})$/, 'YMDHMS'),
    exact(/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})$/, 'YMDHMS'),
    exact(/^(\d{2})\/(\d{2})\/(\d{4}) (\d{2}):(\d{2})$/, 'MDYHM'),
    exact(/^(\d{1,2})\/(\d{1,2})\/(\d{4}) (\d{1,2}):(\d{2}) (AM|PM)$/, 'MDYHM12'),
    exact(/^(\d{2})\/(\d{2})\/(\d{4})$/, 'MDY')
  ];
  for (const parse of parsers) {
    const t = parse(raw);
    if (t) return t.toRFC3339();
  }
  return null;
}

// DateUTC is a plain UTC calendar tuple; no timezone arithmetic anywhere in
// this module, so output formatting cannot drift across runtimes.
class DateUTC {
  constructor(
    readonly y: number,
    readonly mo: number,
    readonly d: number,
    readonly h: number,
    readonly mi: number,
    readonly s: number
  ) {}
  toRFC3339(): string {
    const p2 = (n: number): string => String(n).padStart(2, '0');
    return `${this.y}-${p2(this.mo)}-${p2(this.d)}T${p2(this.h)}:${p2(this.mi)}:${p2(this.s)}Z`;
  }
}

function daysInMonth(y: number, mo: number): number {
  if (mo === 2) return (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0 ? 29 : 28;
  return [31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][mo - 1];
}

// CalendarStamp is a raw date/time tuple as captured by one of the layout
// regexes; time fields default to midnight when a layout omits them.
interface CalendarStamp {
  y: number;
  mo: number;
  d: number;
  h?: number;
  mi?: number;
  s?: number;
}

// ResolvedCalendar is a stamp with the omitted time fields filled in.
interface ResolvedCalendar extends CalendarStamp {
  h: number;
  mi: number;
  s: number;
}

function mkDate(stamp: CalendarStamp): DateUTC | null {
  const resolved = resolveStamp(stamp);
  if (!validCalendarDay(resolved) || !validTimeOfDay(resolved)) return null;
  return new DateUTC(resolved.y, resolved.mo, resolved.d, resolved.h, resolved.mi, resolved.s);
}

function resolveStamp(stamp: CalendarStamp): ResolvedCalendar {
  return { y: stamp.y, mo: stamp.mo, d: stamp.d, h: stamp.h ?? 0, mi: stamp.mi ?? 0, s: stamp.s ?? 0 };
}

function validCalendarDay(day: ResolvedCalendar): boolean {
  return day.mo >= 1 && day.mo <= 12 && day.d >= 1 && day.d <= daysInMonth(day.y, day.mo);
}

function validTimeOfDay(time: ResolvedCalendar): boolean {
  return time.h <= 23 && time.mi <= 59 && time.s <= 59;
}

// parseRFC3339 accepts exactly RFC 3339 (fractional seconds optional; Z or a
// numeric offset). A non-Z offset normalizes to UTC, mirroring Go's
// t.UTC().Format(RFC3339).
function parseRFC3339(s: string): DateUTC | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})(\.\d+)?(?:[Zz]|([+-]\d{2}):(\d{2}))$/.exec(s);
  if (!m) return null;
  const base = mkDate({ y: +m[1], mo: +m[2], d: +m[3], h: +m[4], mi: +m[5], s: +m[6] });
  if (!base) return null;
  if (!m[8]) return base;
  const sign = m[8][0] === '-' ? -1 : 1;
  const offMin = sign * (+m[8].slice(1) * 60 + +m[9]);
  const epoch = Date.UTC(base.y, base.mo - 1, base.d, base.h, base.mi, base.s) - offMin * 60_000;
  const t = new Date(epoch);
  return new DateUTC(t.getUTCFullYear(), t.getUTCMonth() + 1, t.getUTCDate(), t.getUTCHours(), t.getUTCMinutes(), t.getUTCSeconds());
}

// exact builds strict parsers for the fixed .NET storage/display shapes.
// "MM" requires two digits, "M/D" one or two, "H" one or two with "PM"
// uppercase-only — matching Go time.Parse's fixed vs flexible numeric widths.
function exact(re: RegExp, shape: 'YMDHMS' | 'MDYHM' | 'MDYHM12' | 'MDY'): (s: string) => DateUTC | null {
  return (s) => {
    const m = re.exec(s);
    if (!m) return null;
    switch (shape) {
      case 'YMDHMS':
        return mkDate({ y: +m[1], mo: +m[2], d: +m[3], h: +m[4], mi: +m[5], s: +m[6] });
      case 'MDYHM':
        return mkDate({ y: +m[3], mo: +m[1], d: +m[2], h: +m[4], mi: +m[5] });
      case 'MDY': {
        return mkDate({ y: +m[3], mo: +m[1], d: +m[2] });
      }
      case 'MDYHM12': {
        let h = +m[4];
        if (h < 1 || h > 12) return null; // a 12-hour clock never shows 0 or 13+
        if (m[6] === 'PM') h = h === 12 ? 12 : h + 12;
        else h = h === 12 ? 0 : h;
        return mkDate({ y: +m[3], mo: +m[1], d: +m[2], h, mi: +m[5] });
      }
    }
  };
}
