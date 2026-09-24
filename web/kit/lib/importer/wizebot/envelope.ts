// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export class WizebotExportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'WizebotExportError';
  }
}

export interface WbRow {
  aliases: string;
  id: string;
  text: string;
  cost: string;
  permHtml: string;
  type: string;
  sound: boolean;
}

export interface WbEnvelope {
  rows: WbRow[];
  malformed: number;
}

const COL = { aliases: 0, id: 1, text: 2, cost: 3, perm: 4, type: 5, sound: 6 } as const;
const MIN_COLUMNS = COL.type + 1;

function cell(row: unknown[], index: number): string {
  const v = row[index];
  if (typeof v === 'string') return v;
  return typeof v === 'number' ? String(v) : '';
}

function flag(row: unknown[], index: number): boolean {
  const v = row[index];
  if (typeof v === 'number') return v !== 0;
  if (typeof v === 'boolean') return v;
  return v === '1';
}

function readRow(raw: unknown): WbRow | null {
  if (!Array.isArray(raw) || raw.length < MIN_COLUMNS) return null;
  return {
    aliases: cell(raw, COL.aliases),
    id: cell(raw, COL.id),
    text: cell(raw, COL.text),
    cost: cell(raw, COL.cost),
    permHtml: cell(raw, COL.perm),
    type: cell(raw, COL.type),
    sound: flag(raw, COL.sound)
  };
}

function decodeJson(bytes: Uint8Array): unknown {
  try {
    return JSON.parse(new TextDecoder('utf-8', { fatal: false }).decode(bytes));
  } catch (err) {
    throw new WizebotExportError(
      `importer/wizebot: the command list is not JSON: ${(err as Error)?.message ?? String(err)}`
    );
  }
}

function listOf(doc: unknown): unknown[] {
  if (Array.isArray(doc)) return doc;
  const data = (doc as Record<string, unknown> | null)?.data;
  if (Array.isArray(data)) return data;
  throw new WizebotExportError(
    'importer/wizebot: not a Wizebot command list: expected a "data" array of command rows'
  );
}

export function decodeEnvelope(bytes: Uint8Array): WbEnvelope {
  const list = listOf(decodeJson(bytes));
  const env: WbEnvelope = { rows: [], malformed: 0 };
  for (const raw of list) {
    const row = readRow(raw);
    if (row === null) env.malformed++;
    else env.rows.push(row);
  }
  if (env.rows.length === 0)
    throw new WizebotExportError(
      'importer/wizebot: this Wizebot streaming website published no custom commands'
    );
  return env;
}

const ROW_TYPES = new Set(['SAY', 'SAY_RANDOM', 'SCREEN', 'ONLY_SOUND', 'ACTION', 'URL']);

function looksLikeRow(row: WbRow): boolean {
  if (!row.aliases.trimStart().startsWith('!')) return false;
  return ROW_TYPES.has(row.type.trim().toUpperCase());
}

export function detectWizebot(bytes: Uint8Array): boolean {
  try {
    return decodeEnvelope(bytes).rows.some(looksLikeRow);
  } catch {
    return false;
  }
}
