// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

type CookieStore = {
  get(name: string): string | undefined;
  delete(name: string, opts: { path: string; secure: boolean }): void;
};

export type HookEvent = {
  cookies: CookieStore;
  url: URL;
  request: Request;
  route: { id: string | null };
};

export type TransactionAgent = {
  setTransactionName(name: string): void;
  addCustomAttributes(atts: Record<string, string | number | boolean>): void;
  setUserID(id: string): void;
};

export type ErrorAgent = {
  noticeError(error: Error, atts: Record<string, string | number>): void;
};

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

export function preloadStrategy(input: { type: string }): boolean {
  return input.type === 'js' || input.type === 'css' || input.type === 'font';
}

export function harden(res: Response, permissionsPolicy: string): void {
  res.headers.set('X-Content-Type-Options', 'nosniff');
  res.headers.set('X-Frame-Options', 'DENY');
  res.headers.set('Referrer-Policy', 'same-origin');
  res.headers.set('Permissions-Policy', permissionsPolicy);
  res.headers.set('Strict-Transport-Security', 'max-age=31536000; includeSubDomains');

  const ct = res.headers.get('content-type') ?? '';
  // Redirects carry no content-type; left cacheable, the CF edge could replay one to another user.
  const isRedirect = res.status >= 300 && res.status < 400;
  if (isRedirect || ct.includes('text/html')) res.headers.set('Cache-Control', 'no-store');
}

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
