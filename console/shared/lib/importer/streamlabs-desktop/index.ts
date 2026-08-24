// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Public surface of the StreamLabs Desktop Chatbot.db importer plus the
// orchestration that mirrors streamlabsdesktop.go: validate the file, walk
// the feature tables, assemble the manifest and diagnostics.
//
// Layout mirrors the Go original: ./dbfile is dbfile.go (open/staging),
// ./parameters is parameters.go ($params + permissions), ./extract is
// extract.go (table readers).

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

  const db = await openDatabase(raw);
  try {
    return parseDatabase(db);
  } finally {
    db.close();
  }
}

function parseDatabase(db: Database): ParseResult {
  const ctx: SectionContext = { db, tables: readSchema(db), diags: [] };

  const manifest: ImportManifest = {};
  const commands = extractCommands(ctx);
  if (commands.length) manifest.commands = commands;
  const timers = extractTimers(ctx);
  if (timers.length) manifest.timers = timers;
  const quotes = extractQuotes(ctx);
  if (quotes.length) manifest.quotes = quotes;

  const diags = ctx.diags;
  diags.push(...missingTableNotes(ctx.tables));
  if (isEmptyStats(statsOf(manifest))) {
    diags.push(manifestWarn('Chatbot.db contained no importable commands, timers or quotes'));
  }
  return { manifest, diagnostics: diags };
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

// Re-exports: the parser's public API, unchanged for every existing import
// specifier (@bagel/shared/importer/streamlabs-desktop and ./streamlabsdesktop).
export { DEFAULT_TIMER_INTERVAL_SECONDS, parseQuoteDate } from './extract';
export { mapPermissionSLCB, translateVariables } from './parameters';
export { StreamLabsDesktopError } from './dbfile';
