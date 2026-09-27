// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { variableReferenceData } from './index';
import { SURFACES } from '../../i18n/builder';
import { lex } from '@bagel/kit/engine/tmpl';
import { rehearseReply } from '@bagel/kit/engine/rehearsal';

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

test('module builder examples use published namespaces and rehearse with their palette samples', () => {
  for (const surface of SURFACES.filter((surface) => surface.id !== 'custom')) {
    const samples = Object.fromEntries(surface.vars.flatMap((variable) => {
      const token = lex(variable.token)[0];
      return token?.kind === 'var' ? [[token.key, variable.sample]] : [];
    }));
    for (const lang of ['en', 'fr'] as const) {
      expect(rehearseReply(surface.example[lang], samples).flatMap((line) => line.segments)
        .filter((segment) => segment.kind === 'unknown'), `${surface.id} ${lang}`).toEqual([]);
    }
  }
});
