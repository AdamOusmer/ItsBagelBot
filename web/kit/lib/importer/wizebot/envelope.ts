// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Envelope layer of the Wizebot parser: what the streaming website's command
// list looks like on the wire, and how one row folds into the record the parse
// layer walks.
//
// The list is NOT a documented API response. It is the payload that renders
// the "Commands" table of a channel's Wizebot streaming website, so it is a
// POSITIONAL array per row rather than a keyed object:
//
//	{"data":[["!Yuki  !Yukizuri","2138083","Retrouves &hellip;","-",
//	          "<span class=\"label label-warning\">VIPs</span>","SAY",0,
//	          "d41d8cd98f00b204e9800998ecf8427e"], …]}
//
//	0 aliases   whitespace-separated, HTML-escaped, first one is the name
//	1 id        Wizebot's own row id, numeric string, unused here
//	2 text      HTML-escaped chat text, may hold \r\n, may be empty
//	3 cost      "-" | "C<n>" (channel currency) | "B<n>" (bits)
//	4 permHtml  "" or one <span class="label …">Label</span> per allowed tier
//	5 type      SAY | SAY_RANDOM | SCREEN | ONLY_SOUND | ACTION | URL
//	6 sound     0/1, whether the command also fires a sound
//	7 category  md5 of the category name, unused here
//
// Columns are read BY POSITION and every one of them tolerantly: an upstream
// page that grows a ninth column, or answers a number where it used to answer
// a string, must not cost the broadcaster their import. A row that is not an
// array, or that is too short to carry a name and a type, is counted as
// malformed and reported once by the parse layer.

export class WizebotExportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'WizebotExportError';
  }
}

// WbRow is one command row in the shape the parse layer wants: strings, never
// undefined, entities still encoded (decoding is ./entities' job and happens
// per field, because the permission column needs its markup stripped first).
export interface WbRow {
  aliases: string;
  id: string;
  text: string;
  cost: string;
  permHtml: string;
  type: string;
  sound: boolean;
}

// WbEnvelope is the decoded list plus the count of rows that carried no
// readable columns, so the parse layer can name the loss once instead of
// swallowing it.
export interface WbEnvelope {
  rows: WbRow[];
  malformed: number;
}

// COL names the positional columns this parser reads. MIN_COLUMNS is what a
// row must carry to be worth reading at all: aliases through type.
const COL = { aliases: 0, id: 1, text: 2, cost: 3, perm: 4, type: 5, sound: 6 } as const;
const MIN_COLUMNS = COL.type + 1;

// cell reads one column as a string. Numbers are accepted and stringified
// (the id column is a string today, but the sound column next to it is a
// number, so the boundary is not one this parser wants to bet on).
function cell(row: unknown[], index: number): string {
  const v = row[index];
  if (typeof v === 'string') return v;
  return typeof v === 'number' ? String(v) : '';
}

// flag reads the sound column, which the live list emits as 0/1.
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
    // fatal:false replaces invalid UTF-8 rather than throwing on it; a syntax
    // error still surfaces, as the list-level failure it is.
    return JSON.parse(new TextDecoder('utf-8', { fatal: false }).decode(bytes));
  } catch (err) {
    throw new WizebotExportError(
      `importer/wizebot: the command list is not JSON: ${(err as Error)?.message ?? String(err)}`
    );
  }
}

// listOf finds the row array. The live list keys it as "data"; a bare array is
// accepted too, since that is what a hand-saved copy of the same payload looks
// like once the wrapper is peeled off.
function listOf(doc: unknown): unknown[] {
  if (Array.isArray(doc)) return doc;
  const data = (doc as Record<string, unknown> | null)?.data;
  if (Array.isArray(data)) return data;
  throw new WizebotExportError(
    'importer/wizebot: not a Wizebot command list: expected a "data" array of command rows'
  );
}

// decodeEnvelope normalizes one command-list payload into rows, or throws
// WizebotExportError when the bytes are not one at all.
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

// looksLikeRow is the shape probe that keeps this parser from claiming another
// source's export: a Wizebot row is a positional array whose name column
// starts with "!" and whose type column is one of the words the streaming
// website emits.
const ROW_TYPES = new Set(['SAY', 'SAY_RANDOM', 'SCREEN', 'ONLY_SOUND', 'ACTION', 'URL']);

function looksLikeRow(row: WbRow): boolean {
  if (!row.aliases.trimStart().startsWith('!')) return false;
  return ROW_TYPES.has(row.type.trim().toUpperCase());
}

// detectWizebot answers whether these bytes are a Wizebot command list without
// throwing.
export function detectWizebot(bytes: Uint8Array): boolean {
  try {
    return decodeEnvelope(bytes).rows.some(looksLikeRow);
  } catch {
    return false;
  }
}
