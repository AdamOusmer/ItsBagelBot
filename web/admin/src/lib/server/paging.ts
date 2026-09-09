// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Query-string paging inputs for the admin list surfaces (audit, users,
// notifications).
//
// Four copies of the same two readers existed, differing only in which
// MAX_PAGES constant they closed over -- so the cap became a parameter and the
// clamping became one function. Both readers exist to keep a crafted `?page=`
// or `?q=` from reaching a service RPC: the page number is what sizes the
// fetch, and the search string is echoed back into the page.

const MAX_SEARCH_LENGTH = 200;

/** Clamp `?page=` into 1..maxPages. Anything non-numeric reads as page 1. */
export function parsePage(raw: string | null, maxPages: number): number {
  const page = Number(raw ?? '1');
  if (!Number.isFinite(page)) return 1;
  return Math.min(Math.max(Math.trunc(page), 1), maxPages);
}

/** Trim `?q=` and cap its length before it reaches a service RPC. */
export function normalizeSearch(raw: string | null): string {
  return (raw ?? '').trim().slice(0, MAX_SEARCH_LENGTH);
}
