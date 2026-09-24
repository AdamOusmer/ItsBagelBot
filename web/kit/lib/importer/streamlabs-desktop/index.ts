// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Database } from 'sql.js';
import type { ImportDiagnostic, ImportManifest } from '../types';
import { isEmptyStats, stats as statsOf } from '../validate';
import {
  StreamLabsDesktopError,
  hasSQLiteMagic,
  openDatabase,
  readSchema,
  getSqlJs,
  listTables,
  findTable,
  COMMAND_TABLE_CANDIDATES,
  TIMER_TABLE_CANDIDATES,
  QUOTE_TABLE_CANDIDATES
} from './dbfile';
import {
  manifestWarn,
  missingTableNotes,
  extractCommands,
  extractTimers,
  extractQuotes,
  type SectionContext
} from './extract';

export interface ParseResult {
  manifest: ImportManifest;
  diagnostics: ImportDiagnostic[];
}

export async function parseStreamLabsDesktop(raw: Uint8Array): Promise<ParseResult> {
  if (!hasSQLiteMagic(raw)) {
    throw new StreamLabsDesktopError(
      'streamlabsdesktop: not a SQLite 3 database (missing "SQLite format 3" header)'
    );
  }

  const db = await openDatabase(raw);
  try {
    return parseDatabase(db);
  } finally {
    db.close();
  }
}

function parseDatabase(db: Database): ParseResult {
  const ctx: SectionContext = { db, tables: readSchema(db), diags: [], fetchDefs: new Map() };

  const manifest: ImportManifest = {};
  const commands = extractCommands(ctx);
  if (commands.length) manifest.commands = commands;
  const timers = extractTimers(ctx);
  if (timers.length) manifest.timers = timers;
  const quotes = extractQuotes(ctx);
  if (quotes.length) manifest.quotes = quotes;
  if (ctx.fetchDefs.size) manifest.fetches = [...ctx.fetchDefs.values()];

  const diags = ctx.diags;
  diags.push(...missingTableNotes(ctx.tables));
  if (isEmptyStats(statsOf(manifest))) {
    diags.push(manifestWarn('Chatbot.db contained no importable commands, timers or quotes'));
  }
  return { manifest, diagnostics: diags };
}

const MIN_SQLITE_PAGE_BYTES = 4096;

export async function detectStreamLabsDesktop(raw: Uint8Array): Promise<boolean> {
  if (!hasSQLiteMagic(raw) || raw.length < MIN_SQLITE_PAGE_BYTES) return false;

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

export function fetchStreamLabsDesktop(file: Uint8Array): Uint8Array {
  if (file.length === 0) {
    throw new StreamLabsDesktopError(
      'streamlabsdesktop: upload your Chatbot.db file (%APPDATA%\\AnkhBot\\Twitch\\Chatbot.db)'
    );
  }
  return file;
}

export { DEFAULT_TIMER_INTERVAL_SECONDS, parseQuoteDate } from './extract';
export { mapPermissionSLCB, translateVariables } from './parameters';
export { StreamLabsDesktopError } from './dbfile';
