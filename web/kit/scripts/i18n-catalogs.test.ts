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
  return { plugin, emitted, urls: exported('urls') as Record<string, Record<string, string>>, names: exported('localeNames') };
}

const GROUPS = { discord: ['discord'], editor: ['vars', 'replyVars'] };

describe('i18nCatalogs', () => {
  const { emitted, urls, names } = build({ exclude: ['admin'], groups: GROUPS });

  test('splits every locale into core and groups that cover the tree without overlap', () => {
    for (const locale of consoleLocales()) {
      const parts = Object.values(urls[locale]).map((url) => JSON.parse(emitted[url.slice(1)]));
      const namespaces = parts.flatMap((part) => Object.keys(part));
      expect(new Set(namespaces).size).toBe(namespaces.length);
      const expected = Object.keys(readConsoleTree(locale)).filter((namespace) => namespace !== 'admin');
      expect(namespaces.sort()).toEqual(expected.sort());
    }
  });

  test('keeps excluded namespaces out of every part', () => {
    expect(Object.values(urls.en).some((url) => 'admin' in JSON.parse(emitted[url.slice(1)]))).toBe(false);
  });

  test('puts group namespaces only in their group', () => {
    expect(Object.keys(JSON.parse(emitted[urls.en.discord.slice(1)]))).toEqual(['discord']);
    expect(Object.keys(JSON.parse(emitted[urls.en.core.slice(1)]))).not.toContain('vars');
  });

  test('names assets by content so server and browser builds agree', () => {
    const again = build({ exclude: ['admin'], groups: GROUPS });
    expect(again.urls).toEqual(urls);
    expect(urls.en.core).toMatch(/^\/_app\/immutable\/i18n\/en\.core\.[0-9a-f]{10}\.json$/);
  });

  test('exposes locale display names without loading catalogs', () => {
    expect(names.fr).toBe(readConsoleTree('fr').lang && (readConsoleTree('fr').lang as { name: string }).name);
  });

  test('admin builds without exclusions keep the admin namespace in core', () => {
    const full = build({});
    expect(Object.keys(JSON.parse(full.emitted[full.urls.en.core.slice(1)]))).toContain('admin');
  });
});
