// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { readFlatCatalogs } from '@bagel/kit/i18n/fs';
import { SITE_FOOTER, SITE_LEGAL, SITE_NAV, resolveSiteColumns, resolveSiteLinks } from '@bagel/kit/site-links';
import { GROUP_ORDER } from '../lib/variables';

const en = readFlatCatalogs('website').en;
const SRC = join(import.meta.dir, '..');

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) return sources(full);
    return /\.(astro|ts|js|mjs)$/.test(name) && !name.endsWith('.test.ts') ? [full] : [];
  });
}

function collect(pattern: RegExp, group: number): Map<string, string> {
  const found = new Map<string, string>();
  for (const file of sources(SRC)) {
    for (const match of readFileSync(file, 'utf8').matchAll(pattern)) found.set(match[group], relative(SRC, file));
  }
  return found;
}

function labelsOf(resolve: (ctx: Parameters<typeof resolveSiteLinks>[1]) => void): string[] {
  const labels: string[] = [];
  resolve({ path: (p: string) => p, langQuery: '', isActive: () => false, label: (k: string) => (labels.push(k), k) });
  return labels;
}

const navLabels = labelsOf((ctx) => resolveSiteLinks(SITE_NAV, ctx));
const footerLabels = labelsOf((ctx) => {
  resolveSiteColumns(SITE_FOOTER, ctx);
  resolveSiteLinks(SITE_LEGAL, ctx);
});

const DYNAMIC: Record<string, string[]> = {
  'nav.': navLabels.map((k) => `nav.${k}`),
  'footer.': footerLabels.map((k) => `footer.${k}`),
  'guides.variables.group.': GROUP_ORDER.map((g) => `guides.variables.group.${g}`),
};

describe('website catalog keys used by the marketing site', () => {
  test('every literal t() key exists in locales/en/website', () => {
    const literal = collect(/\bt\(\s*(['"])([^'"\n]+)\1/g, 2);
    for (const [key, file] of collect(/useTranslations\([^)]*\)\(\s*(['"])([^'"\n]+)\1/g, 2)) literal.set(key, file);
    const missing = [...literal].filter(([key]) => !(key in en)).map(([key, file]) => `${key} (${file})`);
    expect(missing).toEqual([]);
  });

  test('every template-built key has a known source and resolves', () => {
    const prefixes = collect(/\bt\(\s*`([^`$]*)\$\{/g, 1);
    expect([...prefixes.keys()].filter((prefix) => !(prefix in DYNAMIC))).toEqual([]);
    for (const prefix of prefixes.keys()) {
      const resolved = DYNAMIC[prefix].filter((key) => key.startsWith(prefix));
      expect(resolved.filter((key) => !(key in en))).toEqual([]);
    }
  });
});
