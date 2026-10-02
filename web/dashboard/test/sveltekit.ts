// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { Glob } from 'bun';
import { mock } from 'bun:test';
import { join } from 'node:path';
import { STATIC_LOCALES, staticText } from '../../kit/lib/i18n/static';
import type { Locale } from '../../kit/lib/i18n/types';

const LIB = join(import.meta.dir, '../src/lib');

export const privateEnv: Record<string, string | undefined> = {};

function aliasLib(): void {
  for (const file of new Glob('**/*.ts').scanSync(LIB)) {
    if (file.endsWith('.test.ts')) continue;
    const real = join(LIB, file);
    mock.module(`$lib/${file.slice(0, -'.ts'.length)}`, () => require(real));
  }
}

function stubI18n(): void {
  const isLocale = (value: unknown): value is Locale => typeof value === 'string' && STATIC_LOCALES.includes(value);
  mock.module('@bagel/kit/i18n', () => ({
    DEFAULT_LOCALE: 'en',
    LOCALES: STATIC_LOCALES,
    LOCALE_COOKIE: 'locale',
    isLocale,
    localeName: (code: string) => code,
    detectLocale: () => 'en',
    ensureCatalog: async () => {},
    translateList: () => [],
    translate: (locale: Locale, key: string, params: Record<string, string | number> = {}) =>
      Object.entries(params).reduce(
        (text, [name, value]) => text.split(`{${name}}`).join(String(value)),
        staticText(locale, key)
      )
  }));
}

export function stubSvelteKit(options: { dev?: boolean } = {}): void {
  process.env.NEW_RELIC_ENABLED = 'false';
  process.env.LOG_LEVEL = 'silent';
  aliasLib();
  stubI18n();
  mock.module('$app/environment', () => ({ browser: false, building: false, dev: options.dev ?? false }));
  mock.module('$app/navigation', () => ({ afterNavigate: () => {}, beforeNavigate: () => {} }));
  mock.module('$app/state', () => ({ navigating: {}, page: {}, updated: {} }));
  mock.module('$app/forms', () => ({ enhance: () => ({ destroy: () => {} }) }));
  mock.module('$env/dynamic/private', () => ({ env: privateEnv }));
}
