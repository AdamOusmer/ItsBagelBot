// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { seoHost } from '#lib/seo-host';

export const NOT_FOUND_PATH = '/not-found';
export const ERROR_PATH = '/error';

const SHOWN_CODES: ReadonlySet<number> = new Set([401, 403, 500, 502, 503]);

type PageUrl = Pick<URL, 'pathname' | 'hostname'> & { searchParams: Pick<URLSearchParams, 'get'> };

export function errorCode(raw: string | null): number {
  const code = Number(raw);
  return SHOWN_CODES.has(code) ? code : 500;
}

function isErrorStatus(status: number): boolean {
  return status >= 500 || SHOWN_CODES.has(status);
}

function onErrorPage(url: PageUrl): boolean {
  return url.pathname === NOT_FOUND_PATH || url.pathname === ERROR_PATH;
}

// Public pages keep their in-place 404: it is the response the edge caches and search engines index.
function ownsErrors(url: PageUrl, routeId: string | null): boolean {
  if (onErrorPage(url) || seoHost(url) !== 'dashboard') return false;
  return !(routeId ?? '').startsWith('/(public)');
}

function withLang(path: string, url: PageUrl): string {
  const lang = url.searchParams.get('lang');
  if (!lang) return path;
  const joiner = path.includes('?') ? '&' : '?';
  return `${path}${joiner}lang=${encodeURIComponent(lang)}`;
}

export function errorTarget(url: PageUrl, routeId: string | null, status: number): string | null {
  if (!ownsErrors(url, routeId)) return null;
  if (status === 404) return withLang(NOT_FOUND_PATH, url);
  if (isErrorStatus(status)) return withLang(`${ERROR_PATH}?code=${status}`, url);
  return null;
}
