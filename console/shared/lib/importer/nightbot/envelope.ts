// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Envelope layer of the Nightbot parser: what a saved export may look like,
// and how each accepted shape folds into one NbEnvelope the parse layer walks.
//
// Nightbot ships no "export" button, so the file a broadcaster brings here is
// whatever they saved out of the REST API (api.nightbot.tv/1/commands,
// /1/timers, /1/spam_protection): one endpoint's response, several of them
// stapled into one object, or a bare array of command rows from a community
// exporter. Wire shapes per https://api-docs.nightbot.tv/.

export class NightbotExportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'NightbotExportError';
  }
}

// NbEnvelope is the normalized view every accepted shape collapses to.
export interface NbEnvelope {
  commands: NbRow[];
  timers: NbRow[];
  blacklist: string[];
}

export type NbRow = Record<string, unknown>;

export const isObj = (v: unknown): v is NbRow =>
  v !== null && typeof v === 'object' && !Array.isArray(v);

export const asStr = (v: unknown): string => (typeof v === 'string' ? v : '');

export const asNum = (v: unknown): number | undefined =>
  typeof v === 'number' && Number.isFinite(v) ? v : undefined;

// looksLikeCommand / looksLikeTimer are the shape probes that keep this parser
// from claiming another bot's export: Nightbot rows are the only ones in the
// supported set pairing a `message` with a `name` (StreamElements uses
// command/reply, Moobot identifier/text), and only its timers carry an
// interval alongside a message.
export function looksLikeCommand(row: NbRow): boolean {
  return typeof row.message === 'string' && typeof row.name === 'string';
}

export function looksLikeTimer(row: NbRow): boolean {
  return typeof row.message === 'string' && row.interval !== undefined;
}

function stripBom(bytes: Uint8Array): Uint8Array {
  return bytes.length >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf
    ? bytes.subarray(3)
    : bytes;
}

function decodeJson(bytes: Uint8Array): unknown {
  try {
    // fatal:false replaces invalid UTF-8 rather than throwing on it; a syntax
    // error still surfaces, as the export-level failure it is.
    return JSON.parse(new TextDecoder('utf-8', { fatal: false }).decode(stripBom(bytes)));
  } catch (err) {
    throw new NightbotExportError(
      `importer/nightbot: not a JSON export file: ${(err as Error)?.message ?? String(err)}`
    );
  }
}

// rowsOf pulls one named collection out of the document. The API answers with
// {"_total":N,"commands":[…]}, a hand-stapled bundle nests those responses
// under the same names ({"commands":{"_total":N,"commands":[…]}}), and terser
// exporters carry just the array — all three land here as the array.
function rowsOf(doc: NbRow, key: string): NbRow[] {
  const node = doc[key];
  if (Array.isArray(node)) return node.filter(isObj);
  const nested = isObj(node) ? node[key] : undefined;
  return Array.isArray(nested) ? nested.filter(isObj) : [];
}

// blacklistTerms harvests spam-protection terms. GET /1/spam_protection
// answers with a list of filters keyed by type, of which "blacklist" carries
// the words; a saved single filter or a bare term array is accepted too.
function blacklistTerms(doc: NbRow): string[] {
  const out: string[] = [];
  const take = (v: unknown): void => {
    if (Array.isArray(v)) for (const t of v) if (typeof t === 'string') out.push(t);
  };
  take(doc.blacklist);
  for (const filter of spamFilters(doc)) take(filter.blacklist);
  return out;
}

function spamFilters(doc: NbRow): NbRow[] {
  const node = doc.spam_protection;
  if (Array.isArray(node)) return node.filter(isObj);
  const nested = isObj(node) ? node.spam_protection : undefined;
  return Array.isArray(nested) ? nested.filter(isObj) : [];
}

// A root-level array is read as commands: every exporter that dumps a bare
// array dumps the command list (timers are keyed in each observed shape), and
// probing per row would make one mixed file import as two collections the
// broadcaster never asked for.
function arrayEnvelope(doc: unknown[]): NbEnvelope {
  const commands = doc.filter(isObj);
  if (!commands.some(looksLikeCommand))
    throw new NightbotExportError(
      'importer/nightbot: that array holds no Nightbot commands (each row needs name and message)'
    );
  return { commands, timers: [], blacklist: [] };
}

// decodeEnvelope normalizes any accepted save-out into one NbEnvelope, or
// throws NightbotExportError when the file is not one at all.
export function decodeEnvelope(bytes: Uint8Array): NbEnvelope {
  const doc = decodeJson(bytes);
  if (Array.isArray(doc)) return arrayEnvelope(doc);
  if (!isObj(doc))
    throw new NightbotExportError(
      'importer/nightbot: not a Nightbot export: JSON must be an object or an array'
    );

  const env: NbEnvelope = {
    commands: rowsOf(doc, 'commands'),
    timers: rowsOf(doc, 'timers'),
    blacklist: blacklistTerms(doc)
  };
  if (isEmpty(env))
    throw new NightbotExportError(
      'importer/nightbot: no commands, timers or spam-protection terms found — save the response of api.nightbot.tv/1/commands (and /1/timers) into one file'
    );
  return env;
}

function isEmpty(env: NbEnvelope): boolean {
  return env.commands.length === 0 && env.timers.length === 0 && env.blacklist.length === 0;
}

// detectNightbot answers whether these bytes are a Nightbot save-out without
// throwing. Shape probes decide: a keyed collection alone is not enough, since
// {"commands":[…]} is also StreamElements' envelope.
export function detectNightbot(bytes: Uint8Array): boolean {
  let env: NbEnvelope;
  try {
    env = decodeEnvelope(bytes);
  } catch {
    return false;
  }
  if (env.commands.some(looksLikeCommand)) return true;
  if (env.timers.some(looksLikeTimer)) return true;
  return env.blacklist.length > 0;
}
