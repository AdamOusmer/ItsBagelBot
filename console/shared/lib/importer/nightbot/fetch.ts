// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Fetch layer of the Nightbot config-import source: pulls a channel's custom
// commands, timers and spam-protection filters straight from the Nightbot API
// with an OAuth access token, replacing the old save-a-devtools-response flow.
// The result is one stapled envelope object shaped exactly like the bundles
// ./envelope already decodes ({commands: <response>, timers: <response>,
// spam_protection: <filters>}), so the parse layer is untouched.
//
// Wire shapes per https://api-docs.nightbot.tv/: GET /1/commands and
// /1/timers answer {"_total":N,"status":200,"<name>":[…]}; GET
// /1/spam_protection answers with the filter list (each filter of type
// "blacklist" carries the term array ./envelope harvests). The token comes
// from Nightbot's authorization_code OAuth flow (scopes: commands timers
// spam_protection), never from user paste.

// defaultAPIBase is Nightbot's production root. Injectable so tests point
// fetchNightbot at a local server, mirroring the StreamElements fetch layer.
export const DEFAULT_API_BASE = 'https://api.nightbot.tv';

// FETCH_TIMEOUT_MS bounds each upstream call via AbortController. Three
// sequential calls happen per fetch, so worst case is ~30s.
export const FETCH_TIMEOUT_MS = 10_000;

// MAX_RESPONSE_BODY caps how much of one upstream reply is read into memory —
// same posture as the StreamElements fetch layer: orders of magnitude past any
// real command list while bounding a hostile or broken server response.
const MAX_RESPONSE_BODY = 16 << 20;

// MAX_TOKEN_LEN: Nightbot access tokens are short opaque strings today; 512
// leaves room without letting a pasted novel reach the transport. TOKEN_SHAPE
// refuses interior whitespace/control bytes (header-injection bait).
export const MAX_TOKEN_LEN = 512;
const TOKEN_SHAPE = /^[\x21-\x7e]+$/;

export class NightbotFetchError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'NightbotFetchError';
  }
}

export interface FetchOptions {
  baseUrl?: string;
  timeoutMs?: number;
}

// NbFetchEnvelope is the stapled-bundle shape ./envelope's decodeEnvelope
// accepts: whole API responses under commands/timers (rowsOf unwraps the
// nesting) and the extracted filter rows under spam_protection.
export interface NbFetchEnvelope {
  commands: unknown;
  timers: unknown;
  spam_protection: unknown[];
}

// fetchNightbot reads the account's config over the REST API with the OAuth
// access token. Commands and timers are load-bearing (a failure throws);
// spam protection degrades to an empty filter list — the blacklist is a bonus
// collection and a scope hiccup must not cost the broadcaster their commands.
export async function fetchNightbot(
  accessToken: string,
  opts: FetchOptions = {}
): Promise<NbFetchEnvelope> {
  const token = accessToken.trim();
  if (token === '')
    throw new NightbotFetchError('nightbot: access token is required (connect your Nightbot account)');
  if (token.length > MAX_TOKEN_LEN || !TOKEN_SHAPE.test(token))
    throw new NightbotFetchError('nightbot: access token is malformed (reconnect your Nightbot account)');

  const client: NbClient = { base: opts.baseUrl || DEFAULT_API_BASE, token, opts };
  const commands = await nbGet<unknown>(client, '/1/commands');
  const timers = await nbGet<unknown>(client, '/1/timers');
  return { commands, timers, spam_protection: await spamFiltersOrEmpty(client) };
}

// spamFiltersOrEmpty pulls /1/spam_protection best-effort and normalizes the
// response to the bare filter array ./envelope walks. The live API keys the
// list as "filters"; older saved shapes used "spam_protection", so both are
// accepted rather than betting the blacklist on one spelling.
async function spamFiltersOrEmpty(client: NbClient): Promise<unknown[]> {
  let doc: Record<string, unknown>;
  try {
    doc = await nbGet<Record<string, unknown>>(client, '/1/spam_protection');
  } catch {
    return [];
  }
  const list = doc?.filters ?? doc?.spam_protection;
  return Array.isArray(list) ? list : [];
}

// NbClient binds the access token to the API root for the sequenced reads.
interface NbClient {
  base: string;
  token: string;
  opts: FetchOptions;
}

async function nbGet<T>(client: NbClient, path: string): Promise<T> {
  const timeoutMs = client.opts.timeoutMs ?? FETCH_TIMEOUT_MS;
  const abort = new AbortController();
  const timer = setTimeout(() => abort.abort(), timeoutMs);
  try {
    return await requestJSON<T>(client, path, abort.signal);
  } catch (err) {
    if (err instanceof NightbotFetchError) throw err;
    const reason =
      err instanceof Error && err.name === 'AbortError' ? 'request timed out' : String(err);
    throw new NightbotFetchError(`${path}: ${reason}`);
  } finally {
    clearTimeout(timer);
  }
}

async function requestJSON<T>(client: NbClient, path: string, signal: AbortSignal): Promise<T> {
  const res = await fetch(client.base + path, {
    method: 'GET',
    headers: { Authorization: `Bearer ${client.token}`, Accept: 'application/json' },
    signal
  });
  const text = await readCapped(res, MAX_RESPONSE_BODY, path);
  if (res.status !== 200)
    throw new NightbotFetchError(`${path} returned ${res.status}: ${snippet(text)}${authHint(res.status)}`);
  try {
    return JSON.parse(text) as T;
  } catch (err) {
    throw new NightbotFetchError(`${path}: decoding response: ${(err as Error).message}`);
  }
}

// readCapped reads the body but refuses to buffer more than cap bytes, so a
// hostile server streaming forever cannot balloon the dashboard pod's memory.
async function readCapped(res: Response, cap: number, path: string): Promise<string> {
  try {
    const reader = res.body?.getReader();
    if (!reader) return await res.text();
    return await decodeCappedChunks(reader, cap, path);
  } catch (err) {
    if (err instanceof NightbotFetchError) throw err;
    throw new NightbotFetchError(`${path}: reading response: ${String(err)}`);
  }
}

async function decodeCappedChunks(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  cap: number,
  path: string
): Promise<string> {
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > cap) {
      await reader.cancel();
      throw new NightbotFetchError(`${path}: reading response: body exceeds ${cap} bytes`);
    }
    chunks.push(value);
  }
  return joinChunks(chunks, total);
}

function joinChunks(chunks: Uint8Array[], total: number): string {
  const merged = new Uint8Array(total);
  let at = 0;
  for (const c of chunks) {
    merged.set(c, at);
    at += c.byteLength;
  }
  return new TextDecoder().decode(merged);
}

// authHint appends remediation only where the cause is almost certainly the
// token: Nightbot access tokens live 30 days and revocation reads identical to
// expiry without this nudge.
function authHint(status: number): string {
  return status === 401 || status === 403
    ? ' (reconnect your Nightbot account — the authorization expired or was revoked)'
    : '';
}

// snippet collapses an upstream error body into one short single-line fragment.
const MAX_BODY_SNIPPET = 256;
function snippet(body: string): string {
  let s = body;
  if (s.length > MAX_BODY_SNIPPET) s = s.slice(0, MAX_BODY_SNIPPET) + '…';
  return s.replaceAll('\n', ' ').split(/\s+/).filter(Boolean).join(' ');
}
