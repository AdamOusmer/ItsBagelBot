// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const PUBLIC_PREFIXES = ['/auth/login', '/auth/callback', '/auth/logout', '/login', '/healthz', '/readyz'];
const BOT_OAUTH_ROUTES = ['/auth/bot/login', '/auth/bot/callback', '/auth/bot/done'];

export function isPublicAdminRoute(pathname: string): boolean {
  // Bot OAuth proves the configured bot identity in its callback; it creates no staff session.
  return BOT_OAUTH_ROUTES.includes(pathname) || PUBLIC_PREFIXES.some(
    (prefix) => pathname === prefix || pathname.startsWith(prefix + '/')
  );
}
