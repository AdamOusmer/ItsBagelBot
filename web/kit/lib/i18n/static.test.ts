// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { STATIC_LOCALES, staticText } from './static';
import { localeOptions } from '../site-links';

test('the six locales use native names and registered switcher choices', () => {
  expect(STATIC_LOCALES).toEqual(['de', 'en', 'es', 'fr', 'pt-br', 'ru']);
  const options = localeOptions(STATIC_LOCALES, 'pt-br', new URL('https://dashboard.itsbagelbot.com/settings?tab=profile'), (code) => staticText(code, 'lang.name'));
  expect(options.map((option) => option.title)).toEqual(['Deutsch', 'English', 'Español', 'Français', 'Português (Brasil)', 'Русский']);
  expect(options.find((option) => option.current)?.code).toBe('pt-br');
  for (const option of options) {
    const url = new URL(option.href, 'https://dashboard.itsbagelbot.com');
    expect(url.pathname).toBe('/settings');
    expect(url.searchParams.get('tab')).toBe('profile');
    expect(url.searchParams.get('lang')).toBe(option.code);
    expect(option.flag).toBeDefined();
  }
});
test.each(['es', 'pt-br', 'de', 'ru'])('%s serves translated console metadata', (locale) => {
  expect(staticText(locale, 'vars.user.name')).not.toBe(staticText('en', 'vars.user.name'));
  expect(staticText(locale, 'vars.user.name')).not.toBe('vars.user.name');
});
test('unknown locales keep the English fallback', () => {
  expect(staticText('xx', 'vars.user.name')).toBe(staticText('en', 'vars.user.name'));
});
