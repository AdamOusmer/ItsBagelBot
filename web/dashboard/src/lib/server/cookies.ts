// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const YEAR_SECONDS = 60 * 60 * 24 * 365;

export const STATE_TTL_SECONDS = 600;

export function baseCookie(url: URL, path = '/') {
  return { path, httpOnly: true, secure: url.protocol === 'https:', sameSite: 'lax' as const };
}

export function cookieOptions(url: URL, maxAge: number, path = '/') {
  return { ...baseCookie(url, path), maxAge };
}

export function deleteOptions(url: URL) {
  return { path: '/', secure: url.protocol === 'https:' };
}
