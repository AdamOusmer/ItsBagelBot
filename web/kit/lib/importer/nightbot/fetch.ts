// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const DEFAULT_API_BASE = 'https://api.nightbot.tv';

export const FETCH_TIMEOUT_MS = 10_000;

const MAX_RESPONSE_BODY_BYTES = 16 << 20;

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

export interface NbFetchEnvelope {
  commands: unknown;
  timers: unknown;
  spam_protection: unknown[];
}

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
  const text = await readCapped(res, MAX_RESPONSE_BODY_BYTES, path);
  if (res.status !== 200)
    throw new NightbotFetchError(`${path} returned ${res.status}: ${snippet(text)}${authHint(res.status)}`);
  try {
    return JSON.parse(text) as T;
  } catch (err) {
    throw new NightbotFetchError(`${path}: decoding response: ${(err as Error).message}`);
  }
}

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

function authHint(status: number): string {
  return status === 401 || status === 403
    ? ' (reconnect your Nightbot account: the authorization expired or was revoked)'
    : '';
}

const MAX_BODY_SNIPPET = 256;
function snippet(body: string): string {
  let s = body;
  if (s.length > MAX_BODY_SNIPPET) s = s.slice(0, MAX_BODY_SNIPPET) + '…';
  return s.replaceAll('\n', ' ').split(/\s+/).filter(Boolean).join(' ');
}
