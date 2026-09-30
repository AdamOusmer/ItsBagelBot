// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { dirname, join, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { assembleCatalogs, type CatalogFile } from './tree';
import { flatCatalogs, type FlatCatalogs, type FlatTree } from './flat';
import type { Locale, MessageTree } from './types';

// Walks up: the marketing SSG build runs this module from web/marketing/dist/.prerender/chunks.
function findLocalesDir(start: string): string {
  for (let dir = start; ; dir = dirname(dir)) {
    if (existsSync(join(dir, 'locales', 'manifest.json'))) return join(dir, 'locales');
    if (dirname(dir) === dir) throw new Error(`i18n: no locales/manifest.json above ${start}`);
  }
}

export const LOCALES_DIR = findLocalesDir(dirname(fileURLToPath(import.meta.url)));

export function surfaceLocales(surface: string): Locale[] {
  return readdirSync(LOCALES_DIR, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && existsSync(join(LOCALES_DIR, entry.name, surface)))
    .map((entry) => entry.name)
    .sort();
}

export function consoleLocales(): Locale[] {
  return surfaceLocales('console');
}

function readJson(file: string): unknown {
  try {
    return JSON.parse(readFileSync(file, 'utf8'));
  } catch (err) {
    throw new Error(`i18n: ${file}: invalid JSON: ${(err as Error).message}`);
  }
}

export function readConsoleFiles(locale: Locale): CatalogFile[] {
  const dir = join(LOCALES_DIR, locale, 'console');
  return readdirSync(dir, { recursive: true, encoding: 'utf8' })
    .filter((name) => name.endsWith('.json'))
    .sort()
    .map((name) => ({ path: `${locale}/console/${name.split(sep).join('/')}`, content: readJson(join(dir, name)) }));
}

export function readConsoleTree(locale: Locale): MessageTree {
  return assembleCatalogs(readConsoleFiles(locale))[locale] ?? {};
}

export function readFlatCatalogs(surface: string): FlatCatalogs {
  const files: Record<string, FlatTree> = {};
  for (const locale of surfaceLocales(surface)) {
    const dir = join(LOCALES_DIR, locale, surface);
    for (const name of readdirSync(dir, { recursive: true, encoding: 'utf8' }).filter((n) => n.endsWith('.json')).sort()) {
      files[`locales/${locale}/${surface}/${name.split(sep).join('/')}`] = readJson(join(dir, name)) as FlatTree;
    }
  }
  return flatCatalogs(files, surface);
}
