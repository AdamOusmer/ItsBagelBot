// Request-time hook helpers shared by both consoles' hooks.server.ts.
//
// Framework-agnostic seam, mirroring config.ts: structural types instead of
// SvelteKit imports, and the New Relic agent is injected by the caller instead
// of imported here (newrelic is an app dependency, not a shared one). Each app
// keeps its own policy decisions (staff gate, rate limits, locale) and composes
// these building blocks in its own handle().

type CookieStore = {
  get(name: string): string | undefined;
  delete(name: string, opts: { path: string; secure: boolean }): void;
};

/** The slice of SvelteKit's RequestEvent these helpers read. */
export type HookEvent = {
  cookies: CookieStore;
  url: URL;
  request: Request;
  route: { id: string | null };
};

/** The slice of the New Relic agent used to tag web transactions. */
export type TransactionAgent = {
  setTransactionName(name: string): void;
  addCustomAttributes(atts: Record<string, string | number | boolean>): void;
  setUserID(id: string): void;
};

/** The slice of the New Relic agent used to report server errors. */
export type ErrorAgent = {
  noticeError(error: Error, atts: Record<string, string | number>): void;
};

/** Opens the session cookie, dropping an expired/invalid cookie eagerly so the
 *  browser stops replaying it. Returns the decoded session or null. */
export function openSessionCookie<S>(
  event: HookEvent,
  cookie: string,
  open: (value: string) => S | null
): S | null {
  const raw = event.cookies.get(cookie);
  const session = raw ? open(raw) : null;
  if (raw && !session) {
    event.cookies.delete(cookie, { path: '/', secure: event.url.protocol === 'https:' });
  }
  return session;
}

/** Names the New Relic web transaction by SvelteKit route (so per-id paths
 *  group instead of exploding by raw URL) and tags request/session context for
 *  faceting. */
export function tagTransaction(
  nr: TransactionAgent,
  event: HookEvent,
  session: { user_id?: string | number } | null
): void {
  nr.setTransactionName(`${event.request.method} ${event.route.id ?? event.url.pathname}`);
  nr.addCustomAttributes({
    'route.id': event.route.id ?? 'unmatched',
    'http.method': event.request.method,
    'enduser.authenticated': !!session
  });
  if (session?.user_id) nr.setUserID(String(session.user_id));
}

/** SvelteKit preloads js + css by default; adding fonts makes the SSR'd <head>
 *  warm the woff2 files in parallel with the bundle instead of waiting for CSS
 *  to parse first. Fewer round-trips, less FOUT/CLS on first paint. */
export function preloadStrategy(input: { type: string }): boolean {
  return input.type === 'js' || input.type === 'css' || input.type === 'font';
}

/** Sets the security headers SvelteKit's CSP config does not own, then makes
 *  HTML pages AND navigation redirects uncacheable. A SvelteKit redirect
 *  carries no content-type or Cache-Control, so the text/html check alone
 *  would leave 30x responses cacheable: the CF edge could then pin a stale "go
 *  here" (e.g. /login or a post-action target) and replay it to the wrong
 *  user/session after a deploy. __data.json is already `private, no-store` and
 *  hashed /_app assets are served by sirv with their own immutable caching. */
export function harden(res: Response, permissionsPolicy: string): void {
  res.headers.set('X-Content-Type-Options', 'nosniff');
  res.headers.set('X-Frame-Options', 'DENY');
  res.headers.set('Referrer-Policy', 'same-origin');
  res.headers.set('Permissions-Policy', permissionsPolicy);
  res.headers.set('Strict-Transport-Security', 'max-age=31536000; includeSubDomains');

  const ct = res.headers.get('content-type') ?? '';
  const isRedirect = res.status >= 300 && res.status < 400;
  if (isRedirect || ct.includes('text/html')) res.headers.set('Cache-Control', 'no-store');
}

/** Sends unexpected server errors to New Relic with route/status context. 4xx
 *  are expected (auth/not-found) and left out so the error rate tracks real
 *  faults. */
export function noticeServerError(
  nr: ErrorAgent,
  error: unknown,
  event: { route?: { id: string | null }; url: URL },
  status: number
): void {
  if (status < 500) return;
  nr.noticeError(error instanceof Error ? error : new Error(String(error)), {
    'route.id': event.route?.id ?? event.url.pathname,
    'http.status': status
  });
}
