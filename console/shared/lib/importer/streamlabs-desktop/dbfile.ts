// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// sql.js open/staging layer for the StreamLabs Desktop Chatbot.db reader:
// lazy WASM load, magic sniffing, schema discovery, whole-table scans and
// the shared diagnostic/integer primitives the sibling modules reuse.
//
// Decision records on read-only semantics, query timeout bounds and why
// sql.js (not node:sqlite) live beside their consumers in this module.

import { createRequire } from 'node:module';
import type { Database, QueryExecResult, SqlJsStatic } from 'sql.js';
import type { ImportDiagnostic } from '../types';

export const warnDiag = (item_index: number, code: string, message: string): ImportDiagnostic => ({
  severity: 'warn',
  item_index,
  code,
  message
});
export const errDiag = (item_index: number, code: string, message: string): ImportDiagnostic => ({
  severity: 'error',
  item_index,
  code,
  message
});

// --- sqlite access -----------------------------------------------------------


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
export function getSqlJs(): Promise<SqlJsStatic> {
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

export function hasSQLiteMagic(raw: Uint8Array): boolean {
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
export const MAX_SCAN_ROWS = 20000;
// Longest legitimate consumer of one cell is a command response (5 lines x 500
// chars); anything past 4096 bytes is junk by construction.
const MAX_CELL_LEN = 4096;
// Candidates are three fixed names, so anything past this is hostile noise.
const MAX_SCHEMA_ENTRIES = 10000;

// Table/column candidates, lower-cased (SQLite identifiers match
// case-insensitively; sqlite_master preserves the created spelling). Ordered
// most-likely-first. Copied verbatim from extract.go; provenance of every
// entry lives in FORMAT_NOTES.md in the Go package's git history.
export const COMMAND_TABLE_CANDIDATES = ['commands', 'command'];
export const TIMER_TABLE_CANDIDATES = ['timers', 'timer'];
export const QUOTE_TABLE_CANDIDATES = ['quotes', 'quote'];

// --- sqlite helpers ----------------------------------------------------------

// listTables returns the database's tables as a lower-cased name set (values
// are the original spellings). Virtual tables are excluded: querying one hands
// the file control to a module (FTS5, csv, ...) whose parsers were never part
// of this threat model; real SLCB tables are plain rowid tables, so skipping
// CREATE VIRTUAL TABLE entries costs nothing legitimate and denies a crafted
// upload the chance to name a hostile vtab "Commands".
export function listTables(db: Database): Map<string, string> {
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
export function findTable(tables: Map<string, string>, candidates: string[]): string {
  for (const c of candidates) {
    const real = tables.get(c.toLowerCase());
    if (real !== undefined) return real;
  }
  return '';
}

// FieldRead is one candidate-column probe's outcome: the raw cell text (empty
// when nothing usable was found) and whether a usable value existed.
interface FieldRead {
  value: string;
  present: boolean;
}

// Row is one result row keyed by lower-cased column name. first returns the
// first candidate column that exists AND holds a non-empty value, so a schema
// that renamed e.g. Response→ResponseText still parses without code changes.
export class Row {
  readonly cols: Record<string, string>;
  constructor(cells: Record<string, string>) {
    this.cols = cells;
  }
  first(candidates: string[]): FieldRead {
    for (const c of candidates) {
      const v = this.cols[c.toLowerCase()];
      if (v !== undefined && v.trim() !== '') return { value: v, present: true };
    }
    return { value: '', present: false };
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
export function selectAll(db: Database, table: string): { rows: Row[]; truncated: boolean } {
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

export function goAtoi(s: string): number | null {
  if (!/^[+-]?\d+$/.test(s)) return null;
  const n = Number(s);
  return Number.isSafeInteger(n) ? n : null;
}

// openDatabase opens the staged bytes read-only; a cold upload has no
// producer WAL/SHM sidecars, so there is nothing to recover and nothing to
// write back.
export async function openDatabase(raw: Uint8Array): Promise<Database> {
  try {
    const SQL = await getSqlJs();
    return new SQL.Database(raw);
  } catch (err) {
    throw new StreamLabsDesktopError(`streamlabsdesktop: opening database read-only: ${String(err)}`);
  }
}

// readSchema wraps listTables with the parse-failed prose the handler
// surfaces when sqlite refuses a hostile file.
export function readSchema(db: Database): Map<string, string> {
  try {
    return listTables(db);
  } catch (err) {
    throw new StreamLabsDesktopError(`streamlabsdesktop: reading schema: ${String(err)}`);
  }
}
