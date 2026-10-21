// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { consoleLocales, readConsoleTree } from '../lib/i18n/tree-fs';
import { i18nCatalogs } from './i18n-catalogs';

type Hook = (this: unknown, ...args: unknown[]) => unknown;
type Hooks = { resolveId: Hook; load: Hook; generateBundle: Hook };

function build(options: Parameters<typeof i18nCatalogs>[0]) {
  const plugin = i18nCatalogs(options) as unknown as Hooks;
  const emitted: Record<string, string> = {};
  const emitFile = (file: { fileName: string; source: string }) => {
    emitted[file.fileName] = file.source;
  };
  plugin.generateBundle.call({ environment: { name: 'client' }, emitFile });
  const entry = plugin.load(String(plugin.resolveId('virtual:bagel-i18n')), { ssr: false }) as string;
  const exported = (name: string) => JSON.parse(new RegExp(`export const ${name} = (.*);`).exec(entry)![1]);
  return { plugin, emitted, urls: exported('urls') as Record<string, string>, names: exported('localeNames') };
}

describe('i18nCatalogs', () => {
  const { emitted, urls, names } = build({ exclude: ['admin'] });
  const catalog = (locale: string) => JSON.parse(emitted[urls[locale].slice(1)]);

  test('emits one file per locale holding the whole tree minus exclusions', () => {
    for (const locale of consoleLocales()) {
      const expected = Object.keys(readConsoleTree(locale)).filter((namespace) => namespace !== 'admin');
      expect(Object.keys(catalog(locale)).sort()).toEqual(expected.sort());
    }
  });

  test('renders a key present only in English as English for other locales', () => {
    const english = catalog('en');
    const french = catalog('fr');
    expect(french.lang.name).toBe('Français');
    expect(Object.keys(french).sort()).toEqual(Object.keys(english).sort());
  });

  test('names assets by content so server and browser builds agree', () => {
    expect(build({ exclude: ['admin'] }).urls).toEqual(urls);
    expect(urls.en).toMatch(/^\/_app\/immutable\/i18n\/en\.[0-9a-f]{10}\.json$/);
  });

  test('exposes locale display names without loading catalogs', () => {
    expect(names.fr).toBe('Français');
  });

  test('generated modules hold no raw code-unsafe characters yet evaluate to the same values', () => {
    const hostile = '</script>\u{2028}\u{2029}a/b';
    const tree = { lang: { name: hostile }, greeting: hostile };
    const { plugin } = build({ source: { locales: () => ['en', hostile], read: () => tree } });
    const generate = (id: string, ssr: boolean) => {
      const code = plugin.load(`\0${id}`, { ssr }) as string;
      expect(code).not.toMatch(/<\/script>|[\u{2028}\u{2029}]/u);
      return code;
    };

    const entry = generate('virtual:bagel-i18n', true);
    const names = JSON.parse(/export const localeNames = (.*);/.exec(entry)![1]);
    expect(names[hostile]).toBe(hostile);
    expect(JSON.parse(/export const locales = (.*);/.exec(entry)![1])).toEqual(['en', hostile]);
    const loaderKeys = new Function(`${entry.replaceAll('export const ', 'const ')}\nreturn Object.keys(loaders);`)();
    expect(loaderKeys).toEqual(['en', hostile]);
    const dataModule = generate('virtual:bagel-i18n/data/en', false);
    expect(new Function(dataModule.replace('export default', 'return'))()).toEqual(tree);
  });

  test('admin builds without exclusions keep the admin namespace', () => {
    const full = build({});
    expect(Object.keys(JSON.parse(full.emitted[full.urls.en.slice(1)]))).toContain('admin');
  });
});
