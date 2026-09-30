// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { STATIC_LOCALES } from '../../../../kit/lib/i18n/static';

let changes: string[] = [];
let failSave = false;
mock.module('@bagel/kit/i18n', () => ({
  LOCALE_COOKIE: 'locale',
  isLocale: (code: unknown) => typeof code === 'string' && STATIC_LOCALES.includes(code)
}));
mock.module('$lib/server/services', () => ({
  setLocale: async (user: string, locale: string) => {
    changes.push(`save:${user}:${locale}`);
    if (failSave) throw new Error('save failed');
  }
}));
const { POST } = await import('./+server');

async function submit(locale: string, session: object | null = { user_id: '42' }, next = '/settings') {
  const form = new FormData();
  form.set('to', locale);
  form.set('next', next);
  const event = {
    request: new Request('https://dashboard.itsbagelbot.com/lang', { method: 'POST', body: form }),
    url: new URL('https://dashboard.itsbagelbot.com/lang'),
    locals: { session, locale: 'en' },
    cookies: { set: (_name: string, value: string) => changes.push(`cookie:${value}`) }
  } as unknown as Parameters<typeof POST>[0];
  try { await POST(event); } catch (result) { return result; }
}

describe('language preference save', () => {
  beforeEach(() => { changes = []; failSave = false; });
  test.each(['es', 'pt-br', 'de', 'ru'])('%s saves before the cookie and returns to settings', async (locale) => {
    expect(await submit(locale)).toMatchObject({ status: 303, location: '/settings' });
    expect(changes).toEqual([`save:42:${locale}`, `cookie:${locale}`]);
  });
  test('backend failure leaves the browser preference unchanged', async () => {
    failSave = true;
    expect(await submit('de')).toMatchObject({ message: 'save failed' });
    expect(changes).toEqual(['save:42:de']);
  });
  test('anonymous switching updates only the cookie', async () => {
    await submit('pt-br', null);
    expect(changes).toEqual(['cookie:pt-br']);
  });
  test('unknown locales change neither saved preference nor cookie', async () => {
    await submit('xx');
    expect(changes).toEqual([]);
  });
  test('view-as keeps the interface English while saving the target preference', async () => {
    await submit('ru', { user_id: '42', impersonator_id: '9' });
    expect(changes).toEqual(['save:42:ru', 'cookie:en']);
  });
  test('external return paths cannot redirect off-site', async () => {
    expect(await submit('es', null, 'https://example.com')).toMatchObject({ status: 303, location: '/' });
  });
});
