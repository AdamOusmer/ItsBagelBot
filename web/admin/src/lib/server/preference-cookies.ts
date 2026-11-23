// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const YEAR_SECONDS = 365 * 24 * 60 * 60;

export const CURSOR_COOKIE = 'bagel_cursor';

export function preferenceCookieOptions(url: URL) {
  return {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax' as const,
    maxAge: YEAR_SECONDS
  };
}
