// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { readdir, readFile } from 'node:fs/promises';
import { join, relative, resolve } from 'node:path';

const repoRoot = resolve(import.meta.dir, '../../..');

const SCAN_ROOTS = [
  'ui/styles', 'ui/astro', 'ui/svelte', 'ui/lib',
  'web/marketing/src', 'web/dashboard/src', 'web/admin/src', 'web/kit',
];

const SKIPPED_DIRS = new Set(['node_modules', '.svelte-kit', 'dist', 'build']);

const SCANNED_EXT = ['.css', '.svelte', '.astro', '.ts'];

const isScanned = (name: string) =>
  !name.endsWith('.d.ts') && SCANNED_EXT.some((ext) => name.endsWith(ext));

const isTestFile = (name: string) => /\.(test|spec)\.(ts|js)$/.test(name);

// Allowlist for a legitimate locale-coupled style: path -> reason.
const ALLOWLIST = new Map<string, string>([]);

const STYLE_BLOCK = /<style\b[^>]*>([\s\S]*?)<\/style>/g;

function styleText(rel: string, source: string): string {
  if (rel.endsWith('.css')) return source;
  return [...source.matchAll(STYLE_BLOCK)].map((m) => m[1]).join('\n');
}

const LANG_PSEUDO = /:lang\s*\(/;
const LANG_ATTR_SELECTOR = /\[\s*lang\s*(\^=|\|=|=)/;
const HTML_LANG_SELECTOR = /html\s*\[\s*lang\b/;

function cssOffenses(css: string): string[] {
  const hits: string[] = [];
  if (LANG_PSEUDO.test(css)) hits.push(':lang() selector');
  if (HTML_LANG_SELECTOR.test(css)) hits.push('html[lang] selector');
  else if (LANG_ATTR_SELECTOR.test(css)) hits.push('[lang=] attribute selector');
  return hits;
}

const CLASS_OR_STYLE_TOKEN = /\b(class(?:Name)?|style)(?::[\w-]+)?\s*=/;
const LOCALE_COND =
  /\b(lang|locale)\s*===?\s*['"][\w-]+['"]|['"][\w-]+['"]\s*===?\s*\b(lang|locale)\b|\$\{\s*(lang|locale)\s*\}/;

function markupOffenses(source: string): string[] {
  const hits: string[] = [];
  const lines = source.split('\n');
  for (const [i, line] of lines.entries()) {
    if (CLASS_OR_STYLE_TOKEN.test(line) && LOCALE_COND.test(line)) {
      hits.push(`line ${i + 1}: class/style bound to locale (${line.trim()})`);
    }
  }
  return hits;
}

async function* files(dir: string): AsyncGenerator<string> {
  let entries;
  try {
    entries = await readdir(dir, { withFileTypes: true });
  } catch {
    return;
  }
  for (const entry of entries) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!SKIPPED_DIRS.has(entry.name)) yield* files(full);
    } else if (isScanned(entry.name) && !isTestFile(entry.name)) {
      yield full;
    }
  }
}

async function scan(): Promise<Map<string, string[]>> {
  const byFile = new Map<string, string[]>();
  for (const root of SCAN_ROOTS) {
    for await (const file of files(join(repoRoot, root))) {
      const rel = relative(repoRoot, file);
      if (ALLOWLIST.has(rel)) continue;
      const source = await readFile(file, 'utf8');
      const hits = [...cssOffenses(styleText(rel, source)), ...markupOffenses(source)];
      if (hits.length) byFile.set(rel, hits);
    }
  }
  return byFile;
}

describe('layout stays locale-agnostic', () => {
  test('no :lang()/[lang=]/html[lang] selector and no class or style bound to a locale code', async () => {
    const byFile = await scan();
    const offenders = [...byFile].flatMap(([rel, hits]) => hits.map((h) => `${rel}: ${h}`));
    expect(
      offenders,
      'A layout rule keyed on the locale code hides a real breakpoint/overflow bug behind a ' +
        'per-language patch. Fix the layout so it fits every locale, or add a reasoned ' +
        'allowlist entry in web/kit/test/locale-agnostic-styles.test.ts.',
    ).toEqual([]);
  });

  test('every allowlisted file still exists', async () => {
    const missing: string[] = [];
    for (const rel of ALLOWLIST.keys()) {
      try {
        await readFile(join(repoRoot, rel));
      } catch {
        missing.push(rel);
      }
    }
    expect(missing, 'Remove allowlist entries for deleted files').toEqual([]);
  });
});

describe('hero title keys exist for every manifest locale', () => {
  test('hero.title1..3 are present and non-empty for each locale in locales/manifest.json', async () => {
    const manifest: string[] = JSON.parse(await readFile(join(repoRoot, 'locales/manifest.json'), 'utf8'));
    const missing: string[] = [];
    for (const locale of manifest) {
      let hero: Record<string, unknown>;
      try {
        hero = JSON.parse(await readFile(join(repoRoot, 'locales', locale, 'website/hero.json'), 'utf8'));
      } catch {
        missing.push(`${locale}: locales/${locale}/website/hero.json is missing or invalid`);
        continue;
      }
      for (const key of ['title1', 'title2', 'title3']) {
        const value = hero[key];
        if (typeof value !== 'string' || value.trim() === '') {
          missing.push(`${locale}: hero.${key} is missing or empty`);
        }
      }
    }
    expect(missing).toEqual([]);
  });
});
