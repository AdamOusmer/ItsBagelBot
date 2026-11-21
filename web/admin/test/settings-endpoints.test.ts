// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import type { Cookies } from '@sveltejs/kit';
import { consoleLocales } from '@bagel/kit/i18n/fs';
import { isPublicAdminRoute } from '../src/lib/server/public-routes';

const locales = consoleLocales();
mock.module('virtual:bagel-i18n', () => ({ locales, localeNames: {}, loaders: {}, urls: {}, sliced: false }));

const moderator = { id: '42', login: 'mod', display_name: 'Mod', role: 'moderator' };
mock.module('#lib/server/access', () => ({
  requireAdmin: async (session: { user_id: string } | null) => session?.user_id === moderator.id ? moderator : null
}));

const { POST: lang } = await import('../src/routes/lang/+server');
const { POST: cursor } = await import('../src/routes/cursor/+server');
const handlers = { lang, cursor };

type Submission = {
  route: keyof typeof handlers;
  body: BodyInit;
  mediaType?: string;
  userId?: string | null;
  secure?: boolean;
};

async function submit({ route, body, mediaType, userId = moderator.id, secure = true }: Submission) {
  const url = new URL(`${secure ? 'https:' : 'http:'}//admin.example/${route}`);
  const headers = new Headers({ origin: url.origin });
  if (mediaType) headers.set('content-type', mediaType);
  const request = new Request(url, { method: 'POST', headers, body });
  const writes: Parameters<Cookies['set']>[] = [];
  const cookies: Pick<Cookies, 'set'> = {
    set: (...args) => {
      writes.push(args);
    }
  };
  const locals = { session: userId ? { user_id: userId } : null };
  const result = await Promise.resolve(handlers[route]({ cookies, locals, request, url } as never))
    .catch((error: unknown) => error);
  return { result, writes };
}

function cookieOptions(secure: boolean) {
  return { httpOnly: true, maxAge: 31536000, path: '/', sameSite: 'lax' as const, secure };
}

test.each(locales)('POST /lang saves %s and preserves the return path', async (locale) => {
  const next = '/settings?tab=display#language';
  const body = new URLSearchParams({ to: locale, next });
  const { result, writes } = await submit({ route: 'lang', body });
  expect(result).toMatchObject({ status: 303, location: next });
  expect(writes).toEqual([['locale', locale, cookieOptions(true)]]);
});

test.each(['https://example.net/', '//example.net/', ''])('POST /lang falls back to / for %s', async (next) => {
  const body = new FormData();
  body.set('to', 'fr');
  body.set('next', next);
  const { result, writes } = await submit({ route: 'lang', body, secure: false });
  expect(result).toMatchObject({ status: 303, location: '/' });
  expect(writes).toEqual([['locale', 'fr', cookieOptions(false)]]);
});

test.each([
  { enabled: true, value: '1', secure: true },
  { enabled: false, value: '0', secure: false }
])('POST /cursor saves $enabled as $value', async ({ enabled, value, secure }) => {
  const body = JSON.stringify({ enabled });
  const { result, writes } = await submit({ route: 'cursor', body, mediaType: 'application/json; charset=utf-8', secure });
  expect(result).toMatchObject({ status: 204, body: null });
  expect(writes).toEqual([['bagel_cursor', value, cookieOptions(secure)]]);
});

const invalid: (Submission & { label: string })[] = [
  { label: 'an unknown locale', route: 'lang', body: new URLSearchParams({ to: 'xx' }) },
  { label: 'a missing locale', route: 'lang', body: new URLSearchParams() },
  { label: 'a non-form locale body', route: 'lang', body: '{', mediaType: 'application/json' },
  ...[
    { label: 'malformed JSON', body: '{' },
    { label: 'a missing cursor setting', body: '{}' },
    { label: 'a non-boolean cursor setting', body: '{"enabled":"true"}' }
  ].map((entry) => ({ ...entry, route: 'cursor' as const, mediaType: 'application/json' })),
  { label: 'a plain text cursor body', route: 'cursor', body: '{"enabled":true}', mediaType: 'text/plain' }
];

test.each(invalid)('POST /$route rejects $label', async (submission) => {
  const { result, writes } = await submit(submission);
  expect(result).toMatchObject({ status: 400 });
  expect(writes).toEqual([]);
});

const routes = [
  { route: 'lang', body: new URLSearchParams({ to: 'fr' }) },
  { route: 'cursor', body: '{"enabled":true}', mediaType: 'application/json' }
] satisfies Submission[];

const unauthorized = routes.flatMap((submission) => [
  { ...submission, userId: null, label: 'an anonymous request' },
  { ...submission, userId: '12', label: 'a session without a staff role' }
]);

test.each(unauthorized)('POST /$route rejects $label', async (submission) => {
  const { result, writes } = await submit(submission);
  expect(result).toMatchObject({ status: 403 });
  expect(writes).toEqual([]);
});

test.each(routes)('/$route is outside the public route allowlist', ({ route }) => {
  expect(isPublicAdminRoute(`/${route}`)).toBe(false);
});
