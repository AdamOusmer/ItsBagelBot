// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { readdirSync } from 'node:fs';
import { render } from 'svelte/server';
import { CATALOG_FILES } from '../locales/index';
import { createUiI18n, resolveUiLocale, uiText, UI_LOCALES } from '../lib/i18n';
import { uiI18n } from '../astro/i18n';
import I18nHost from './fixtures/i18n-host.svelte';

function keys(tree: object, prefix = ''): string[] {
  return Object.entries(tree).flatMap(([k, v]) =>
    typeof v === 'string' ? [prefix + k] : keys(v as object, `${prefix}${k}.`),
  );
}

describe('catalogs', () => {
  const reference = keys(CATALOG_FILES.en).sort();

  test('French is complete: exactly the English keys, none empty', () => {
    expect(keys(CATALOG_FILES.fr).sort()).toEqual(reference);
    for (const key of reference) expect(uiText('fr', key as never).trim().length).toBeGreaterThan(0);
  });

  test('no locale has a key English lacks', () => {
    for (const code of UI_LOCALES) {
      expect(keys(CATALOG_FILES[code]).filter((k) => !reference.includes(k)), code).toEqual([]);
    }
  });

  test('the generated index covers every locale folder in ui/locales', () => {
    const folders = readdirSync(new URL('../locales/', import.meta.url), { withFileTypes: true })
      .filter((entry) => entry.isDirectory())
      .map((entry) => entry.name);
    expect([...UI_LOCALES].sort() as string[]).toEqual(folders.sort());
  });
});

describe('lookup', () => {
  test('resolves regional and unknown locales', () => {
    expect(resolveUiLocale('fr-CA')).toBe('fr');
    expect(resolveUiLocale('FR')).toBe('fr');
    expect(resolveUiLocale('de')).toBe('en');
    expect(resolveUiLocale(undefined)).toBe('en');
  });

  test('interpolates named placeholders and leaves unknown ones', () => {
    const i18n = createUiI18n(() => 'en', (key) => (key === 'status.live' ? '{n} live, {missing}' : undefined));
    expect(i18n.t('status.live', { n: 3 })).toBe('3 live, {missing}');
  });

  test('an override wins; undefined falls through to the catalog', () => {
    const i18n = createUiI18n(() => 'fr', (key, locale) => (key === 'status.live' ? `live:${locale}` : undefined));
    expect(i18n.t('status.live')).toBe('live:fr');
    expect(i18n.t('action.close')).toBe('Fermer');
  });

  test('locale is read lazily', () => {
    let current = 'en';
    const i18n = createUiI18n(() => current);
    expect(i18n.t('action.close')).toBe('Close');
    current = 'fr';
    expect(i18n.locale).toBe('fr');
    expect(i18n.t('action.close')).toBe('Fermer');
  });
});

describe('adapters', () => {
  test('Svelte components read the locale set by an ancestor', () => {
    const html = render(I18nHost, { props: { locale: 'fr' } }).body;
    expect(html).toContain('Enregistrement…');
    expect(html).toContain('En ligne');
    expect(html).toContain('Modifications non enregistrées');
  });

  test('a host override replaces one string for every component', () => {
    const html = render(I18nHost, {
      props: { locale: 'en', override: (key: string) => (key === 'status.live' ? 'Synced to chat' : undefined) },
    }).body;
    expect(html).toContain('Synced to chat');
    expect(html).toContain('Saving…');
  });

  test('without a provider Svelte components fall back to English', () => {
    const html = render(I18nHost, { props: { locale: 'xx' } }).body;
    expect(html).toContain('Saving…');
  });

  test('Astro adapters follow locals.uiLocale, then Astro.currentLocale', () => {
    expect(uiI18n({ currentLocale: 'fr' }).t('search.clear')).toBe('Effacer la recherche');
    expect(uiI18n({}).t('search.clear')).toBe('Clear search');
    expect(uiI18n({ currentLocale: 'en', locals: { uiLocale: 'fr' } }).t('action.close')).toBe('Fermer');
  });
});
