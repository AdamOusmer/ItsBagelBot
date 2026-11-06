// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { getCollection, type CollectionEntry } from 'astro:content';
import { docsI18n, docsLocales } from '../i18n/ui';
import { resolvedSpaces, type Space } from './spaces';

export type DocEntry = CollectionEntry<'docs'>;

export interface DocPage {
  id: string;
  entry: DocEntry;
  translated: boolean;
  href: string;
  label: string;
}

const entries = await getCollection('docs');
const localePrefixes = docsLocales.filter((code) => code !== 'en').map((code) => `${code}/`);
const englishEntries = entries.filter(({ id }) => !localePrefixes.some((prefix) => id.startsWith(prefix)));
const byId = new Map(entries.map((entry) => [entry.id, entry]));

export const spaces: Space[] = resolvedSpaces(englishEntries.map(({ id }) => id));

export function pagesFor(locale: string): Map<string, DocPage> {
  const { path } = docsI18n(locale);
  return new Map(
    englishEntries.map((english) => {
      const localized = locale === 'en' ? english : byId.get(`${locale}/${english.id}`);
      const entry = localized ?? english;
      const page: DocPage = {
        id: english.id,
        entry,
        translated: localized !== undefined,
        href: path(`/${english.id}/`),
        label: entry.data.sidebar?.label ?? entry.data.title,
      };
      return [english.id, page];
    }),
  );
}
