// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// StreamLabs Desktop Chatbot (SLCB, formerly AnkhBot) config-import source,
// ported one-for-one from app/importer/source/streamlabsdesktop (dbfile.go +
// extract.go + parameters.go + streamlabsdesktop.go) when the standalone
// importer service was folded into the dashboard. The user uploads their
// Chatbot.db SQLite file (typically %APPDATA%\AnkhBot\Twitch\Chatbot.db) and
// parseStreamLabsDesktop translates the custom commands, timers and quotes
// into a canonical ImportManifest.
//
// SERVER-SIDE ONLY. The SQLite reader is sql.js (SQLite compiled to WASM),
// loaded through a lazy dynamic import so neither this module nor its ~1.5MB
// wasm ever enters a client bundle — and so the browser CSP's no-WASM rule
// stays untouched (it governs script-src; server Node/Bun is unaffected).
//
// Decision record — why sql.js and not node:sqlite (2026-08-23):
//   node:sqlite exists in the production base image (distroless nodejs22), but
//   NOT in Bun 1.3.10 ("Could not resolve: node:sqlite"), which runs this
//   repo's dev server (:5173), bun test AND every local verification path —
//   this module's own test suite would need a second runtime just to run.
//   sql.js is pure WASM + JS: zero native-build steps on ARM/Intel images,
//   identical behavior under Bun dev/test and Node prod. Cost: ~1.5MB wasm in
//   the server image (never in client bundles); queries run synchronously
//   in-process — see the timeout note below.
//
// Decision record — read-only semantics: Go staged the upload to a temp file
// and opened it with mode=ro&immutable=1 (an upload is a cold copy; its
// producer's WAL/SHM sidecars are absent, so the driver must not attempt
// journal recovery). sql.js opens the BYTES in memory instead: there is no
// file to write back, no lock, no recovery — ro/immutable hold structurally
// rather than by DSN flag.
//
// Decision record — query timeout: Go bounded every statement at 10s against a
// hostile file. sql.js executes synchronously, so an in-process deadline
// cannot interrupt a running statement. The bound moves up a layer: uploads
// are capped at 20MB before this module runs, reads are capped at 20001 rows x
// 4096-byte cells per table, and three tables are probed — sub-second worst
// case over the fixture corpus versus unbounded row/cell sizes without those
// caps.
//
// Streamlabs never published the database schema, so every table and column
// access here is defensive by design: tables are discovered from sqlite_master
// at runtime against candidate name lists (copied verbatim from extract.go),
// values are read via SELECT * and matched by case-insensitive column NAME
// (never ordinal), and a missing table degrades to a warn diagnostic instead
// of failing the import.

import {
  CODE,
  canonicalizeResponse,
  clampCooldown,
  isEmptyStats,
  mapPermission,
  normalizeName,
  stats as statsOf
} from './validate';
import type { ImportDiagnostic, ImportManifest } from './types';
import type { Database, QueryExecResult, SqlJsStatic } from 'sql.js';

const warnDiag = (item_index: number, code: string, message: string): ImportDiagnostic => ({
  severity: 'warn',
  item_index,
  code,
  message
});
const errDiag = (item_index: number, code: string, message: string): ImportDiagnostic => ({
  severity: 'error',
  item_index,
  code,
  message
});

// --- sqlite access -----------------------------------------------------------

import { createRequire } from 'node:module';

let sqlJsPromise: Promise<SqlJsStatic> | null = null;

// getSqlJs lazily loads the WASM build once per process.
//
// Decision record — why createRequire and not import('sql.js') (2026-08-23):
// this graph is bundled for SSR (@bagel/shared is ssr.noExternal, and
// rolldown ignores ssr.external/rollupOptions.external for deps reachable only
// through it), which inlines the emscripten glue into an ESM chunk whose
// __dirname + module.exports Node refuses to evaluate ("cannot determine
// intended module format"). A runtime CommonJS require is invisible to the
// bundler: dist/sql-wasm.js then evaluates as CJS exactly as upstream ships
// it, with its wasm located beside itself. Works identically under `vite dev`,
// `bun test`, and the adapter-node bundle.
const nodeRequire = createRequire(import.meta.url);

// The CJS entry exports the init factory itself (no ESM default-interop here),
// so require + invoke; emscripten then resolves to the ready database class.
function getSqlJs(): Promise<SqlJsStatic> {
  return (sqlJsPromise ??= Promise.resolve().then(() => {
    const init = nodeRequire('sql.js') as unknown as () => Promise<SqlJsStatic>;
    return init();
  }));
}

export class StreamLabsDesktopError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'StreamLabsDesktopError';
  }
}

// sqliteMagic is the 16-byte header every SQLite 3 database starts with;
// hasSQLiteMagic sniffs it before paying for a real open.
const SQLITE_MAGIC = 'SQLite format 3\x00';

function hasSQLiteMagic(raw: Uint8Array): boolean {
  if (raw.length < SQLITE_MAGIC.length) return false;
  for (let i = 0; i < SQLITE_MAGIC.length; i++) {
    if (raw[i] !== SQLITE_MAGIC.charCodeAt(i)) return false;
  }
  return true;
}

// Scan bounds for untrusted uploads (verbatim from dbfile.go). maxScanRows is
// comfortably above every manifest cap (commands 2000, quotes 5000) plus SLCB's
// realistic table sizes; the +1 in LIMIT detects overflow without reading the
// overflow rows' cells.
const MAX_SCAN_ROWS = 20000;
// Longest legitimate consumer of one cell is a command response (5 lines x 500
// chars); anything past 4096 bytes is junk by construction.
const MAX_CELL_LEN = 4096;
// Candidates are three fixed names, so anything past this is hostile noise.
const MAX_SCHEMA_ENTRIES = 10000;

// Table/column candidates, lower-cased (SQLite identifiers match
// case-insensitively; sqlite_master preserves the created spelling). Ordered
// most-likely-first. Copied verbatim from extract.go; provenance of every
// entry lives in FORMAT_NOTES.md in the Go package's git history.
const COMMAND_TABLE_CANDIDATES = ['commands', 'command'];
const TIMER_TABLE_CANDIDATES = ['timers', 'timer'];
const QUOTE_TABLE_CANDIDATES = ['quotes', 'quote'];

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

// --- parse -------------------------------------------------------------------

export interface ParseResult {
  manifest: ImportManifest;
  diagnostics: ImportDiagnostic[];
}

// parseStreamLabsDesktop translates the Chatbot.db into a manifest plus
// diagnostics. A broken database (wrong magic, unreadable pages) throws — the
// caller wraps that as parse_failed; anything less fatal becomes a diagnostic
// so the healthy items still import. Items are sorted by normalized name
// before indexing so both manifest order and diagnostic indexes stay stable
// across runs on identical bytes (previews re-run; golden tests pin this).
export async function parseStreamLabsDesktop(raw: Uint8Array): Promise<ParseResult> {
  if (!hasSQLiteMagic(raw)) {
    throw new StreamLabsDesktopError(
      'streamlabsdesktop: not a SQLite 3 database (missing "SQLite format 3" header)'
    );
  }

  let db: Database;
  try {
    const SQL = await getSqlJs();
    db = new SQL.Database(raw);
  } catch (err) {
    throw new StreamLabsDesktopError(`streamlabsdesktop: opening database read-only: ${String(err)}`);
  }

  try {
    let tables: Map<string, string>;
    try {
      tables = listTables(db);
    } catch (err) {
      throw new StreamLabsDesktopError(`streamlabsdesktop: reading schema: ${String(err)}`);
    }

    const manifest: ImportManifest = {};
    const diags: ImportDiagnostic[] = [];

    const cmdDiags: ImportDiagnostic[] = [];
    manifest.commands = extractCommands(db, tables, cmdDiags);
    if (!manifest.commands?.length) delete manifest.commands;

    const tmrDiags: ImportDiagnostic[] = [];
    manifest.timers = extractTimers(db, tables, tmrDiags);
    if (!manifest.timers?.length) delete manifest.timers;

    const qtDiags: ImportDiagnostic[] = [];
    manifest.quotes = extractQuotes(db, tables, qtDiags);
    if (!manifest.quotes?.length) delete manifest.quotes;

    diags.push(...cmdDiags, ...tmrDiags, ...qtDiags);

    for (const { label, candidates } of [
      { label: 'commands', candidates: COMMAND_TABLE_CANDIDATES },
      { label: 'timers', candidates: TIMER_TABLE_CANDIDATES },
      { label: 'quotes', candidates: QUOTE_TABLE_CANDIDATES }
    ]) {
      if (findTable(tables, candidates) === '') {
        diags.push(manifestWarn(`no "${label}" table found in Chatbot.db; that section was skipped`));
      }
    }
    if (isEmptyStats(statsOf(manifest))) {
      diags.push(manifestWarn('Chatbot.db contained no importable commands, timers or quotes'));
    }
    return { manifest, diagnostics: diags };
  } finally {
    db.close();
  }
}

// detectStreamLabsDesktop reports whether raw looks like a Chatbot.db: the
// SQLite magic header plus at least one expected feature table actually
// present. Opening the staged bytes keeps Detect side-effect free.
export async function detectStreamLabsDesktop(raw: Uint8Array): Promise<boolean> {
  // Below one SQLite page (4096 bytes) no feature table can exist, so skip
  // the open entirely.
  if (!hasSQLiteMagic(raw) || raw.length < 4096) return false;

  let db: Database;
  try {
    const SQL = await getSqlJs();
    db = new SQL.Database(raw);
  } catch {
    return false;
  }
  try {
    const tables = listTables(db);
    return (
      findTable(tables, COMMAND_TABLE_CANDIDATES) !== '' ||
      findTable(tables, TIMER_TABLE_CANDIDATES) !== '' ||
      findTable(tables, QUOTE_TABLE_CANDIDATES) !== ''
    );
  } catch {
    return false;
  } finally {
    db.close();
  }
}

// fetchStreamLabsDesktop mirrors the Go Fetch: SLCB has no API fetch, the
// uploaded file IS the config. cred is meaningless here.
export function fetchStreamLabsDesktop(file: Uint8Array): Uint8Array {
  if (file.length === 0) {
    throw new StreamLabsDesktopError(
      'streamlabsdesktop: upload your Chatbot.db file (%APPDATA%\\AnkhBot\\Twitch\\Chatbot.db)'
    );
  }
  return file;
}

// --- sqlite helpers ----------------------------------------------------------

// listTables returns the database's tables as a lower-cased name set (values
// are the original spellings). Virtual tables are excluded: querying one hands
// the file control to a module (FTS5, csv, ...) whose parsers were never part
// of this threat model; real SLCB tables are plain rowid tables, so skipping
// CREATE VIRTUAL TABLE entries costs nothing legitimate and denies a crafted
// upload the chance to name a hostile vtab "Commands".
function listTables(db: Database): Map<string, string> {
  const out = new Map<string, string>();
  for (const row of execOne(db, `SELECT name, COALESCE(sql, '') FROM sqlite_master WHERE type = 'table'`).values) {
    const name = String(row[0]);
    const ddl = String(row[1]);
    if (name.startsWith('sqlite_')) continue; // internal tables (sqlite_sequence et al.)
    if (ddl.trim().toUpperCase().startsWith('CREATE VIRTUAL TABLE')) continue;
    out.set(name.toLowerCase(), name);
    if (out.size >= MAX_SCHEMA_ENTRIES) break;
  }
  return out;
}

// findTable returns the actual (as-created) table name for the first candidate
// that exists, or ''. Callers treat '' as "section missing" → diagnostic.
function findTable(tables: Map<string, string>, candidates: string[]): string {
  for (const c of candidates) {
    const real = tables.get(c.toLowerCase());
    if (real !== undefined) return real;
  }
  return '';
}

// Row is one result row keyed by lower-cased column name. first returns the
// first candidate column that exists AND holds a non-empty value, so a schema
// that renamed e.g. Response→ResponseText still parses without code changes.
class Row {
  readonly cols: Record<string, string>;
  constructor(cells: Record<string, string>) {
    this.cols = cells;
  }
  first(candidates: string[]): [value: string, ok: boolean] {
    for (const c of candidates) {
      const v = this.cols[c.toLowerCase()];
      if (v !== undefined && v.trim() !== '') return [v, true];
    }
    return ['', false];
  }
  valueOf(candidates: string[]): string {
    for (const c of candidates) {
      const v = this.cols[c.toLowerCase()];
      if (v !== undefined) return v;
    }
    return '';
  }
}

// selectAll runs SELECT * against table and scans each row into a Row:
// column names exactly as the schema spells them, values rendered via
// cellString. Returns truncated=true when the table had more than
// MAX_SCAN_ROWS rows; callers surface that as a diagnostic.
function selectAll(db: Database, table: string): { rows: Row[]; truncated: boolean } {
  // Table names come from sqlite_master, never from user input, but quote
  // anyway so odd spellings (spaces, keywords) cannot break the query.
  const q = `SELECT * FROM "${table.replaceAll('"', '""')}" LIMIT ${MAX_SCAN_ROWS + 1}`;
  const result = execOne(db, q);

  const out: Row[] = [];
  let truncated = false;
  for (const values of result.values) {
    const cells: Record<string, string> = {};
    result.columns.forEach((c, i) => {
      cells[c.toLowerCase()] = cellString(values[i]);
    });
    out.push(new Row(cells));
    if (out.length === MAX_SCAN_ROWS + 1) {
      out.pop(); // overflow sentinel row: drop it, report truncation
      truncated = true;
      break;
    }
  }
  return { rows: out, truncated };
}

function execOne(db: Database, sql: string): QueryExecResult {
  const results = db.exec(sql);
  return results.length > 0 ? results[0] : { columns: [], values: [] };
}

// cellString renders one SQLite cell as text. sql.js yields number | string |
// Uint8Array | null; numbers render like Go's %v (integral floats stay "2",
// never "2e+00").
function cellString(v: unknown): string {
  if (v === null || v === undefined) return '';
  if (v instanceof Uint8Array) return truncateCell(new TextDecoder().decode(v));
  if (typeof v === 'string') return truncateCell(v);
  return String(v);
}

function truncateCell(s: string): string {
  return s.length > MAX_CELL_LEN ? s.slice(0, MAX_CELL_LEN) : s;
}

function manifestWarn(message: string): ImportDiagnostic {
  return warnDiag(-1, 'manifest_source_note', message);
}

// reindex rewrites item-level diagnostics onto the item's final manifest index
// after the name-sort, so preview highlights the right row.
function reindex(diags: ImportDiagnostic[], idx: number): void {
  for (const d of diags) d.item_index = idx;
}

// readTable selects one feature table and scans it whole; a missing table or
// read failure degrades to an empty list plus one manifest-level diagnostic —
// the shared preamble every extractor used to repeat.
function readTable(db: Database, tables: Map<string, string>, candidates: string[], diags: ImportDiagnostic[]): Row[] {
  const table = findTable(tables, candidates);
  if (table === '') return [];
  try {
    const sel = selectAll(db, table);
    if (sel.truncated) {
      diags.push(manifestWarn(
        `the "${table}" table has more than ${MAX_SCAN_ROWS} rows; only the first ${MAX_SCAN_ROWS} were imported`
      ));
    }
    return sel.rows;
  } catch (err) {
    diags.push(manifestWarn(`could not read the "${table}" table: ${String(err)}`));
    return [];
  }
}

// --- extraction ---------------------------------------------------------------

// extractCommands reads the commands table into canonical commands. Disabled
// rows are skipped (counted into one manifest-level warn); rows with neither
// name nor response are treated as junk and dropped silently.
function extractCommands(db: Database, tables: Map<string, string>, diags: ImportDiagnostic[]): NonNullable<ImportManifest['commands']> {
  const entries: NonNullable<ImportManifest['commands']>[number][] = [];
  const allWarns: ImportDiagnostic[][] = [];
  let disabled = 0;

  for (const r of readTable(db, tables, COMMAND_TABLE_CANDIDATES, diags)) {
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
  const [nameRaw, ok] = r.first(COMMAND_NAME_COLUMNS);
  return !ok || nameRaw.trim() === '';
}

function isDisabledCommandRow(r: Row): boolean {
  const [enabledRaw, hasEnabled] = r.first(COMMAND_ENABLED_COLUMNS);
  return hasEnabled && !TRUTHY.has(enabledRaw.trim().toLowerCase());
}

// buildCommandRow translates one live command row. Canonicalization findings
// carry index 0 here; the post-sort reindex pass rewrites them onto final
// manifest slots.
function buildCommandRow(r: Row): {
  cmd: NonNullable<ImportManifest['commands']>[number];
  warns: ImportDiagnostic[];
} {
  const [nameRaw] = r.first(COMMAND_NAME_COLUMNS);
  const normName = normalizeName(nameRaw);
  const [response] = r.first(COMMAND_RESPONSE_COLUMNS);

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
      'command_script_dependent',
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
  const [permRaw, hasPerm] = r.first(COMMAND_PERM_COLUMNS);
  if (!hasPerm) return;
  const mapped = mapPermissionSLCB(permRaw);
  cmd.permission = mapped.perm as typeof cmd.permission;
  warns.push(...mapped.diags);
}

function applyRowCooldown(r: Row, cmd: NonNullable<ImportManifest['commands']>[number]): void {
  const [cdRaw, hasCd] = r.first(COMMAND_COOLDOWN_COLUMNS);
  if (!hasCd) return;
  const secs = goAtoi(cdRaw.trim());
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
function extractTimers(db: Database, tables: Map<string, string>, diags: ImportDiagnostic[]): NonNullable<ImportManifest['timers']> {
  const entries: NonNullable<ImportManifest['timers']>[number][] = [];
  const allWarns: ImportDiagnostic[][] = [];
  let defaultedInterval = false;

  for (const r of readTable(db, tables, TIMER_TABLE_CANDIDATES, diags)) {
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
  const [message, ok] = r.first(TIMER_MESSAGE_COLUMNS);
  if (!ok || message.trim() === '') return null;

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
  const [mins, hasMins] = r.first(TIMER_MINUTES_COLUMNS);
  if (hasMins) {
    const m = goAtoi(mins.trim());
    if (m !== null && m > 0) return { seconds: m * 60, known: true };
  }
  const [secs, hasSecs] = r.first(TIMER_SECONDS_COLUMNS);
  if (hasSecs) {
    const s = goAtoi(secs.trim());
    if (s !== null && s > 0) return { seconds: s, known: true };
  }
  return { seconds: 0, known: false };
}

// extractQuotes reads saved quotes. ExtraQuotes (the separate user-repurposed
// list feature) is deliberately NOT read: those entries are usually GIF/URL
// collections wired to custom commands, not quotes; importing them as quotes
// would surprise (decision recorded in FORMAT_NOTES.md).
function extractQuotes(db: Database, tables: Map<string, string>, diags: ImportDiagnostic[]): NonNullable<ImportManifest['quotes']> {
  const entries: NonNullable<ImportManifest['quotes']>[number][] = [];
  const allWarns: ImportDiagnostic[][] = [];

  for (const r of readTable(db, tables, QUOTE_TABLE_CANDIDATES, diags)) {
    const parsed = parseQuoteRow(r);
    if (!parsed) continue;
    entries.push(parsed.entry);
    allWarns.push(parsed.warns);
  }

  orderStable(entries, allWarns, (q) => q.text, diags);
  return entries;
}

function parseQuoteRow(r: Row): { entry: NonNullable<ImportManifest['quotes']>[number]; warns: ImportDiagnostic[] } | null {
  const [textRaw, hasText] = r.first(QUOTE_TEXT_COLUMNS);
  const text = textRaw.trim();
  if (!hasText || text === '') return null;

  const q: NonNullable<ImportManifest['quotes']>[number] = { text };
  const warns: ImportDiagnostic[] = [];

  const [author, hasAuthor] = r.first(QUOTE_AUTHOR_COLUMNS);
  if (hasAuthor) q.added_by = author.trim();

  const [dateRaw, hasDate] = r.first(QUOTE_DATE_COLUMNS);
  if (hasDate) {
    const ts = parseQuoteDate(dateRaw);
    // Date format is channel-configurable in SLCB (default shown in its
    // docs is MM/DD/YYYY); an unparseable value must not block the quote
    // itself, it just loses created_at.
    if (ts !== null) q.created_at = ts;
    else warns.push(quoteDateDiag(dateRaw));
  }
  return { entry: q, warns };
}

function quoteDateDiag(dateRaw: string): ImportDiagnostic {
  return warnDiag(
    -1,
    'quote_date_unparsed',
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

function mkDate(y: number, mo: number, d: number, h = 0, mi = 0, s = 0): DateUTC | null {
  if (mo < 1 || mo > 12 || d < 1 || d > daysInMonth(y, mo)) return null;
  if (h > 23 || mi > 59 || s > 59) return null;
  return new DateUTC(y, mo, d, h, mi, s);
}

// parseRFC3339 accepts exactly RFC 3339 (fractional seconds optional; Z or a
// numeric offset). A non-Z offset normalizes to UTC, mirroring Go's
// t.UTC().Format(RFC3339).
function parseRFC3339(s: string): DateUTC | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})(\.\d+)?(?:[Zz]|([+-]\d{2}):(\d{2}))$/.exec(s);
  if (!m) return null;
  const base = mkDate(+m[1], +m[2], +m[3], +m[4], +m[5], +m[6]);
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
        return mkDate(+m[1], +m[2], +m[3], +m[4], +m[5], +m[6]);
      case 'MDYHM':
        return mkDate(+m[3], +m[1], +m[2], +m[4], +m[5]);
      case 'MDY': {
        return mkDate(+m[3], +m[1], +m[2]);
      }
      case 'MDYHM12': {
        let h = +m[4];
        if (h < 1 || h > 12) return null; // a 12-hour clock never shows 0 or 13+
        if (m[6] === 'PM') h = h === 12 ? 12 : h + 12;
        else h = h === 12 ? 0 : h;
        return mkDate(+m[3], +m[1], +m[2], h, +m[5]);
      }
    }
  };
}

// goAtoi mirrors strconv.Atoi: optional sign then digits, nothing else.
function goAtoi(s: string): number | null {
  if (!/^[+-]?\d+$/.test(s)) return null;
  const n = Number(s);
  return Number.isSafeInteger(n) ? n : null;
}

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
        diags: [warnDiag(-1, 'command_permission_adjusted', outcome.reason(raw))]
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
  if (pos >= text.length || text[pos] !== '(') return null;
  let depth = 0;
  for (let i = pos; i < text.length; i++) {
    if (text[i] === '(') depth++;
    else if (text[i] === ')') {
      depth--;
      if (depth === 0) return i + 1;
    }
  }
  return null;
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
  const inner = span.replace(/^\(/, '').replace(/\)$/, '');
  const parts = inner.split(',');
  if (parts.length === 1) {
    const maxV = goAtoi(parts[0].trim());
    if (maxV === null) return null;
    return `{random:1-${maxV}}`;
  }
  if (parts.length === 2) {
    const a = goAtoi(parts[0].trim());
    const b = goAtoi(parts[1].trim());
    if (a === null || b === null) return null;
    return `{random:${Math.min(a, b)}-${Math.max(a, b)}}`;
  }
  return null;
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
