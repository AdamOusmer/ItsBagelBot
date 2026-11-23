// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { crossSiteForm, sameOrigin } from './csrf';

const origin = 'https://dashboard.itsbagelbot.com';
const form = 'application/x-www-form-urlencoded';

type Row = { name: string; method?: string; path?: string; type?: string | null; origin?: string; blocked: boolean };

const rows: Row[] = [
  { name: 'same-origin form', origin, blocked: false },
  { name: 'cross-site form', origin: 'https://example.test', blocked: true },
  { name: 'form with no Origin', blocked: true },
  { name: 'multipart with no Origin', type: 'multipart/form-data; boundary=x', blocked: true },
  { name: 'body with no content type', type: null, blocked: true },
  { name: 'JSON with no Origin', type: 'application/json', blocked: false },
  { name: 'GET', method: 'GET', blocked: false },
  { name: 'one-click POST with no Origin', path: '/u/v1.7.1.marketing.mac', blocked: false },
  { name: 'one-click POST from another site', path: '/u/v1.7.1.marketing.mac', origin: 'https://example.test', blocked: false },
  { name: 'form action on the unsubscribe path with no Origin', path: '/u/v1.7.1.marketing.mac?/default', blocked: true },
  { name: 'form action on the unsubscribe path from another site', path: '/u/v1.7.1.marketing.mac?/default', origin: 'https://example.test', blocked: true },
  { name: 'PUT on the unsubscribe path', method: 'PUT', path: '/u/v1.7.1.marketing.mac', blocked: true },
  { name: 'nested path under /u', path: '/u/a/b', blocked: true },
  { name: 'settings form with no Origin', path: '/settings', blocked: true }
];

test.each(rows)('crossSiteForm: $name', (row) => {
  const url = new URL(`${origin}${row.path ?? '/commands'}`);
  const headers: Record<string, string> = {};
  if (row.type !== null) headers['content-type'] = row.type ?? form;
  if (row.origin) headers.origin = row.origin;
  const request = new Request(url, { method: row.method ?? 'POST', headers, body: row.method === 'GET' ? undefined : 'a=1' });
  expect(crossSiteForm({ request, url })).toBe(row.blocked);
});

test.each([
  { origin, same: true },
  { origin: 'https://example.test', same: false },
  { origin: undefined, same: false }
])('sameOrigin for $origin', ({ origin: sent, same }) => {
  const url = new URL(`${origin}/u/token`);
  const request = new Request(url, { method: 'POST', headers: sent ? { origin: sent } : {} });
  expect(sameOrigin({ request, url })).toBe(same);
});
