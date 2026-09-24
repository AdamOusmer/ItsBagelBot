// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

let sqlJsPromise: Promise<SqlJsStatic> | null = null;

// createRequire, not import(): the SSR bundle would inline sql.js's glue into a chunk Node cannot evaluate.
const nodeRequire = createRequire(import.meta.url);

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

const SQLITE_MAGIC = 'SQLite format 3\x00';

export function hasSQLiteMagic(raw: Uint8Array): boolean {
  if (raw.length < SQLITE_MAGIC.length) return false;
  for (let i = 0; i < SQLITE_MAGIC.length; i++) {
    if (raw[i] !== SQLITE_MAGIC.charCodeAt(i)) return false;
  }
  return true;
}

export const MAX_SCAN_ROWS = 20000;
const MAX_CELL_LEN = 4096;
const MAX_SCHEMA_ENTRIES = 10000;

export const COMMAND_TABLE_CANDIDATES = ['commands', 'command'];
export const TIMER_TABLE_CANDIDATES = ['timers', 'timer'];
export const QUOTE_TABLE_CANDIDATES = ['quotes', 'quote'];

// Virtual tables are skipped: querying one hands the file to a module parser outside this threat model.
export function listTables(db: Database): Map<string, string> {
  const out = new Map<string, string>();
  for (const row of execOne(db, `SELECT name, COALESCE(sql, '') FROM sqlite_master WHERE type = 'table'`).values) {
    const name = String(row[0]);
    const ddl = String(row[1]);
    if (name.startsWith('sqlite_')) continue;
    if (ddl.trim().toUpperCase().startsWith('CREATE VIRTUAL TABLE')) continue;
    out.set(name.toLowerCase(), name);
    if (out.size >= MAX_SCHEMA_ENTRIES) break;
  }
  return out;
}

export function findTable(tables: Map<string, string>, candidates: string[]): string {
  for (const c of candidates) {
    const real = tables.get(c.toLowerCase());
    if (real !== undefined) return real;
  }
  return '';
}

interface FieldRead {
  value: string;
  present: boolean;
}

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

export function selectAll(db: Database, table: string): { rows: Row[]; truncated: boolean } {
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
      out.pop();
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

export async function openDatabase(raw: Uint8Array): Promise<Database> {
  try {
    const SQL = await getSqlJs();
    return new SQL.Database(raw);
  } catch (err) {
    throw new StreamLabsDesktopError(`streamlabsdesktop: opening database read-only: ${String(err)}`);
  }
}

export function readSchema(db: Database): Map<string, string> {
  try {
    return listTables(db);
  } catch (err) {
    throw new StreamLabsDesktopError(`streamlabsdesktop: reading schema: ${String(err)}`);
  }
}
