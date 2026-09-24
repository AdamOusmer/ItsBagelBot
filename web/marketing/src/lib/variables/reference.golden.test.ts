// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, test } from 'bun:test';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { variableReferenceData } from './index';

const FIXTURES: Record<'en' | 'fr', string> = {
  en: join(import.meta.dir, 'testdata/reference.en.golden.json'),
  fr: join(import.meta.dir, 'testdata/reference.fr.golden.json'),
};

function serialize(lang: 'en' | 'fr'): string {
  return JSON.stringify(variableReferenceData(lang), null, 2) + '\n';
}

function regenerate(path: string, got: string): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, got);
}

function assertMatchesFixture(lang: 'en' | 'fr', path: string, got: string): void {
  const want = readFileSync(path, 'utf8');
  if (got === want) return;
  throw new Error(
    `variableReferenceData('${lang}') no longer matches ${path} byte-for-byte. ` +
      `A changed variable catalog or changed copy is a deliberate act: rerun ` +
      `with UPDATE_GOLDEN=1 to regenerate the fixture, then review the diff ` +
      `before committing it.`
  );
}

describe('variableReferenceData golden (lib/variables/testdata)', () => {
  for (const lang of ['en', 'fr'] as const) {
    test(lang, () => {
      const path = FIXTURES[lang];
      const got = serialize(lang);
      if (process.env.UPDATE_GOLDEN === '1') {
        regenerate(path, got);
        return;
      }
      assertMatchesFixture(lang, path, got);
    });
  }
});
