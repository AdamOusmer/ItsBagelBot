// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Interpolated into a host: no dots, slashes or escapes, so it cannot reach another origin.
export const HANDLE_SHAPE = /^[a-z0-9][a-z0-9-]{0,24}$/;

export type SiteUrl = (handle: string) => string;
export const siteUrlFor: SiteUrl = (handle) => `https://${handle}.streaming.lv`;

export const FETCH_TIMEOUT_MS = 10_000;

const MAX_RESPONSE_BODY_BYTES = 16 << 20;

const SESSION_COOKIE = 'wizebot_network';

const BROWSER_UA =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36';

const LIST_TOKEN = /commands_list\.php\?t=([0-9a-f]{64})/;

export class WizebotFetchError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'WizebotFetchError';
  }
}

export interface FetchOptions {
  baseUrl?: SiteUrl;
  timeoutMs?: number;
}

interface Session {
  base: string;
  handle: string;
  timeoutMs: number;
  cookie: string;
}

interface Call {
  url: string;
  init: RequestInit;
  label: string;
  timeoutMs: number;
}

interface Reply {
  status: number;
  headers: Headers;
  text: string;
}

export async function fetchWizebot(handle: string, opts: FetchOptions = {}): Promise<Uint8Array> {
  const login = normalizeHandle(handle);
  const session: Session = {
    base: (opts.baseUrl ?? siteUrlFor)(login),
    handle: login,
    timeoutMs: opts.timeoutMs ?? FETCH_TIMEOUT_MS,
    cookie: ''
  };
  session.cookie = await openSession(session);
  const list = await readList(session, await listToken(session));
  return new TextEncoder().encode(list);
}

function normalizeHandle(raw: string): string {
  const login = raw.trim().toLowerCase();
  if (login === '')
    throw new WizebotFetchError('wizebot: a channel name is required (the subdomain of your streaming website)');
  if (!HANDLE_SHAPE.test(login))
    throw new WizebotFetchError(
      `wizebot: ${JSON.stringify(raw)} is not a Wizebot channel name (letters, digits and dashes, 25 characters at most)`
    );
  return login;
}

async function openSession(session: Session): Promise<string> {
  const res = await send({
    url: `${session.base}/`,
    init: { method: 'GET', redirect: 'manual', headers: { 'User-Agent': BROWSER_UA, Accept: 'text/html' } },
    label: 'opening the streaming website',
    timeoutMs: session.timeoutMs
  });
  if (isRedirect(res.status))
    throw new WizebotFetchError(
      `wizebot: no Wizebot streaming website is enabled for ${JSON.stringify(session.handle)}`
    );
  requireStatus(res.status, 'opening the streaming website');

  const cookie = sessionCookie(res.headers);
  if (cookie === '')
    throw new WizebotFetchError(
      `wizebot: the streaming website for ${JSON.stringify(session.handle)} did not start a session`
    );
  return cookie;
}

async function listToken(session: Session): Promise<string> {
  const res = await send({
    url: `${session.base}/ajax.php`,
    init: {
      method: 'POST',
      headers: {
        'User-Agent': BROWSER_UA,
        Cookie: session.cookie,
        'X-Requested-With': 'XMLHttpRequest',
        'Content-Type': 'application/x-www-form-urlencoded'
      },
      body: 'p=commands'
    },
    label: 'reading the command table',
    timeoutMs: session.timeoutMs
  });
  requireStatus(res.status, 'reading the command table');

  const token = LIST_TOKEN.exec(res.text)?.[1];
  if (!token)
    throw new WizebotFetchError(
      `wizebot: the streaming website for ${JSON.stringify(session.handle)} did not expose a command list`
    );
  return token;
}

async function readList(session: Session, token: string): Promise<string> {
  const res = await send({
    url: `${session.base}/ajax/list/commands_list.php?t=${token}`,
    init: {
      method: 'GET',
      headers: { 'User-Agent': BROWSER_UA, Cookie: session.cookie, Accept: 'application/json, text/plain' }
    },
    label: 'downloading the command list',
    timeoutMs: session.timeoutMs
  });
  requireStatus(res.status, 'downloading the command list');

  const text = res.text;
  if (text.trim() === '')
    throw new WizebotFetchError(
      `wizebot: the command list for ${JSON.stringify(session.handle)} came back empty (the streaming website did not accept the session)`
    );
  return text;
}

function isRedirect(status: number): boolean {
  return status >= 300 && status <= 399;
}

function requireStatus(status: number, label: string): void {
  if (status === 200) return;
  throw new WizebotFetchError(`wizebot: ${label} returned ${status}`);
}

function sessionCookie(headers: Headers): string {
  for (const line of setCookieLines(headers)) {
    const pair = line.split(';', 1)[0].trim();
    if (pair.startsWith(`${SESSION_COOKIE}=`)) return pair;
  }
  return '';
}

function setCookieLines(headers: Headers): string[] {
  const all = headers.getSetCookie?.() ?? [];
  if (all.length > 0) return all;
  const single = headers.get('set-cookie');
  return single === null ? [] : [single];
}

async function send(call: Call): Promise<Reply> {
  const abort = new AbortController();
  const timer = setTimeout(() => abort.abort(), call.timeoutMs);
  try {
    const res = await fetch(call.url, { ...call.init, signal: abort.signal });
    return { status: res.status, headers: res.headers, text: await readCapped(res, call.label) };
  } catch (err) {
    if (err instanceof WizebotFetchError) throw err;
    throw new WizebotFetchError(`wizebot: ${call.label}: ${transportReason(err)}`);
  } finally {
    clearTimeout(timer);
  }
}

function transportReason(err: unknown): string {
  return err instanceof Error && err.name === 'AbortError' ? 'request timed out' : String(err);
}

async function readCapped(res: Response, label: string): Promise<string> {
  const reader = res.body?.getReader();
  if (!reader) return await res.text();

  const decoder = new TextDecoder('utf-8', { fatal: false });
  let text = '';
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > MAX_RESPONSE_BODY_BYTES) {
      await reader.cancel();
      throw new WizebotFetchError(`wizebot: ${label}: the response exceeds ${MAX_RESPONSE_BODY_BYTES} bytes`);
    }
    text += decoder.decode(value, { stream: true });
  }
  return text + decoder.decode();
}
