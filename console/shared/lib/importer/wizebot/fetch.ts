// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Fetch layer of the Wizebot config-import source: pulls a channel's custom
// command list off its Wizebot "streaming website" and hands the raw JSON to
// ./parse.
//
// Decision record - why three calls instead of an API read (verified end to
// end with curl on two live channels, 2026-09-07):
//
//   Wizebot ships no export and no commands endpoint. wapi.wizebot.tv's
//   /api/channels/{API_KEY}/datas answers with last_quote_* fields and counter
//   strings, nothing resembling a command; it also needs a per-channel API key
//   the broadcaster would have to paste, for data we cannot use. The ONLY
//   public surface carrying custom commands is the per-channel streaming
//   website at <login>.streaming.lv, which the streamer enables in the Wizebot
//   panel. Its command table is session bound, so reading it means replaying
//   what the page itself does:
//
//     1. GET https://<login>.streaming.lv/ with a browser-like User-Agent.
//        Answers 200 and Set-Cookie: wizebot_network=<sid>. A channel that
//        never enabled the site answers 30x to /errors/channel_not_found.html,
//        which is why the request is made with redirect:'manual': following it
//        would land on a 200 error page and turn a clear "not enabled" into a
//        confusing "no command list".
//     2. POST /ajax.php, form-encoded, body p=commands, carrying the cookie
//        and X-Requested-With: XMLHttpRequest. Answers an HTML fragment of the
//        table, inside which sits the per-session list URL
//        ajax/list/commands_list.php?t=<64 hex>. The body is load bearing: any
//        other p value (p=c was tried) answers 200 with an empty body, so this
//        call cannot be skipped or guessed around.
//     3. GET /ajax/list/commands_list.php?t=<token> with the same cookie.
//        Answers the JSON ./envelope decodes. WITHOUT the cookie it answers
//        200 and an EMPTY body, never an error, so a lost session reads as
//        "this channel has no commands" unless the cookie is carried.
//
// Decision record - why the cookie is carried by hand: Node and Bun `fetch`
// implement no cookie jar (WHATWG fetch leaves that to the browser), so a
// session established by call 1 is simply not present on calls 2 and 3 unless
// the Set-Cookie value is read off the response and written back as a Cookie
// header. Combined with the silent-empty-body behaviour above, forgetting it
// produces a successful-looking import of zero commands.
//
// Decision record - what the text column actually holds: on some channels the
// published text is the command's public DESCRIPTION rather than the message
// it posts in chat (Wizebot renders the description in the public table when
// the streamer set one, and the wire carries no flag saying which of the two
// it is). Nothing here can tell them apart, so the import brings over what the
// public page shows and the instructions copy tells the broadcaster to check
// the review step. That is also why sound-only and screen-animation rows are
// skipped by ./parse rather than imported as empty commands.

// HANDLE_SHAPE gates the channel name BEFORE it is interpolated into a host.
// A Twitch login is 4-25 characters of [a-z0-9_], and Wizebot's subdomains
// additionally use "-" where a login has "_" (the live example kenth-s), so
// this is the union of both spellings minus everything that could change what
// host is contacted: no dots, no slashes, no percent escapes, no underscores
// at the start. The URL is built from the constant template below and nothing
// else, so a name that passes this gate cannot reach another origin.
export const HANDLE_SHAPE = /^[a-z0-9][a-z0-9-]{0,24}$/;

// siteUrlFor is the one place a Wizebot host is spelled. SiteUrl is injectable
// so tests point the three calls at a local server.
export type SiteUrl = (handle: string) => string;
export const siteUrlFor: SiteUrl = (handle) => `https://${handle}.streaming.lv`;

// FETCH_TIMEOUT_MS bounds each of the three calls via AbortController.
export const FETCH_TIMEOUT_MS = 10_000;

// MAX_RESPONSE_BODY caps how much of one reply is buffered, same posture as
// the StreamElements and Nightbot fetch layers: orders of magnitude past any
// real command list while bounding a hostile or broken server.
const MAX_RESPONSE_BODY = 16 << 20;

// SESSION_COOKIE is the name Wizebot's streaming website sets. Only this one
// is echoed back, so an unrelated cookie the site sets is never carried.
const SESSION_COOKIE = 'wizebot_network';

// BROWSER_UA: the streaming website answers a bare fetch UA with its error
// page. This is the UA the replay was verified with; it identifies the
// importer honestly in the comment token while looking like the browser the
// page expects.
const BROWSER_UA =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36';

// LIST_TOKEN pulls the per-session list URL's token out of the HTML fragment
// call 2 answers. 64 lowercase hex, exactly as observed on both channels.
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

// Session is the state the three calls share: where the site lives, how long
// one call may take, and the cookie call 1 handed out.
interface Session {
  base: string;
  handle: string;
  timeoutMs: number;
  cookie: string;
}

// Call is one HTTP request this layer makes, named so a failure says which of
// the three legs broke without leaking the session token into the message.
interface Call {
  url: string;
  init: RequestInit;
  label: string;
  timeoutMs: number;
}

// Reply is one answered call, already read. The body is consumed inside the
// same timeout as the request itself (see send), which is what stops a server
// that trickles bytes forever from holding the leg open past FETCH_TIMEOUT_MS.
interface Reply {
  status: number;
  headers: Headers;
  text: string;
}

// fetchWizebot returns the raw command-list JSON for one channel, ready for
// parseWizebot. It is the bytes the streaming website served, unmodified.
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

// --- the three calls ---------------------------------------------------------

// openSession performs call 1 and returns the Cookie header value for the
// other two. A redirect here means the channel never enabled its streaming
// website, which is the one refusal a broadcaster can act on themselves.
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

// listToken performs call 2. p=commands is what selects the commands table;
// see the decision record above for why the body cannot be omitted.
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

// readList performs call 3 and returns the JSON text. An empty body here is
// the site's way of saying the session was not accepted, so it is reported as
// that rather than as an empty channel.
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

// --- transport ---------------------------------------------------------------

function isRedirect(status: number): boolean {
  return status >= 300 && status <= 399;
}

function requireStatus(status: number, label: string): void {
  if (status === 200) return;
  throw new WizebotFetchError(`wizebot: ${label} returned ${status}`);
}

// sessionCookie lifts the wizebot_network cookie out of the response headers.
// getSetCookie is the only correct reader (several Set-Cookie headers cannot
// be joined and re-split safely), with the single-header form kept as a
// fallback for runtimes that predate it.
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

// send performs one call under its own AbortController, turning a transport
// failure or a timeout into readable prose that names which leg broke.
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

// readCapped decodes the body as it streams and refuses to buffer more than
// MAX_RESPONSE_BODY, so a hostile or broken server cannot balloon the
// dashboard pod's memory. Decoding incrementally (stream: true) is what keeps
// a multi-byte character split across two chunks intact.
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
    if (total > MAX_RESPONSE_BODY) {
      await reader.cancel();
      throw new WizebotFetchError(`wizebot: ${label}: the response exceeds ${MAX_RESPONSE_BODY} bytes`);
    }
    text += decoder.decode(value, { stream: true });
  }
  return text + decoder.decode();
}
