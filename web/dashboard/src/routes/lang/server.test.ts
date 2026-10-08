// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { STATIC_LOCALES } from '../../../../kit/lib/i18n/static';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

type Session = { user_id: string; impersonator_id?: string } | null;

let changes: string[] = [];
let failSave = false;

const services = await import('../../lib/server/services');
mock.module('$lib/server/services', () => ({
  ...services,
  setLocale: async (user: string, locale: string) => {
    changes.push(`save:${user}:${locale}`);
    if (failSave) throw new Error('save failed');
  }
}));
const { POST } = await import('./+server');

async function submit(locale: string, session: Session = { user_id: '42' }, next = '/settings') {
  const form = new FormData();
  form.set('to', locale);
  form.set('next', next);
  const event = {
    request: new Request('https://dashboard.itsbagelbot.com/lang', { method: 'POST', body: form }),
    url: new URL('https://dashboard.itsbagelbot.com/lang'),
    locals: { session, locale: 'en' },
    cookies: { set: (_name: string, value: string) => changes.push(`cookie:${value}`) }
  };
  try {
    await POST(event as never);
  } catch (result) {
    return result;
  }
}

const cases: { name: string; locale: string; session?: Session; next?: string; failSave?: boolean; want: string[]; thrown?: object }[] = [
  { name: 'view-as keeps the interface English while saving the target preference', locale: 'ru', session: { user_id: '42', impersonator_id: '9' }, want: ['save:42:ru', 'cookie:en'] },
  { name: 'anonymous switching updates only the cookie', locale: 'pt-br', session: null, want: ['cookie:pt-br'] },
  { name: 'unknown locales change neither saved preference nor cookie', locale: 'xx', want: [] },
  { name: 'backend failure leaves the browser preference unchanged', locale: 'de', failSave: true, want: ['save:42:de'], thrown: { message: 'save failed' } },
  { name: 'external return paths cannot redirect off-site', locale: 'es', session: null, next: 'https://example.com', want: ['cookie:es'], thrown: { status: 303, location: '/' } }
];

describe('language preference save', () => {
  beforeEach(() => {
    changes = [];
    failSave = false;
  });

  test.each([...STATIC_LOCALES])('%s saves before the cookie and returns to settings', async (locale) => {
    expect(await submit(locale)).toMatchObject({ status: 303, location: '/settings' });
    expect(changes).toEqual([`save:42:${locale}`, `cookie:${locale}`]);
  });

  test.each(cases)('$name', async ({ locale, session, next, failSave: fails, want, thrown }) => {
    failSave = fails ?? false;
    const result = await submit(locale, session, next);
    expect(changes).toEqual(want);
    if (thrown) expect(result).toMatchObject(thrown);
  });
});
