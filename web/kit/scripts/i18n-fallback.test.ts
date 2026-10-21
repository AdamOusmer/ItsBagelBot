// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import type { MessageTree } from '../lib/i18n/types.ts';
import { i18nCatalogs } from './i18n-catalogs.ts';

const trees: Record<string, object> = {
  en: { lang: { name: 'English' }, only: { english: 'Only in English' }, shared: 'Shared' },
  fr: { lang: { name: 'Français' }, shared: 'Partagé' },
  de: { lang: { name: 'Deutsch' }, shared: 'Geteilt' }
};

const source = { locales: () => Object.keys(trees), read: (locale: string) => structuredClone(trees[locale]) as MessageTree };

test('a key present only in English is served in English by every merged locale', () => {
  const plugin = i18nCatalogs({ source }) as unknown as { generateBundle: (this: unknown) => void };
  const emitted: Record<string, string> = {};
  plugin.generateBundle.call({ environment: { name: 'client' }, emitFile: (f: { fileName: string; source: string }) => (emitted[f.fileName] = f.source) });
  for (const locale of ['fr', 'de']) {
    const file = Object.keys(emitted).find((name) => name.includes(`/${locale}.`))!;
    const tree = JSON.parse(emitted[file]);
    expect(tree.only.english).toBe('Only in English');
    expect(tree.shared).not.toBe('Shared');
  }
});
