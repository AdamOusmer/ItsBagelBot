// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { matchAcceptLanguage } from './accept-language';

describe('matchAcceptLanguage', () => {
  const LOCALES = ['de', 'en', 'es', 'fr', 'pt-br', 'ru'];
  const match = (header: string | null) => matchAcceptLanguage(header, LOCALES);

  test('matches a region locale exactly', () => {
    expect(match('pt-BR,pt;q=0.9,en;q=0.8')).toBe('pt-br');
    expect(match('pt_BR')).toBe('pt-br');
  });

  test('falls back from a region tag to its base locale', () => {
    expect(match('fr-CA,en;q=0.5')).toBe('fr');
  });

  test('matches a base range to a region locale of that language', () => {
    expect(match('pt,en;q=0.5')).toBe('pt-br');
    expect(match('pt-PT,en;q=0.5')).toBe('pt-br');
  });

  test('follows q-weight order across ranges', () => {
    expect(match('en;q=0.4, pt-BR;q=0.9')).toBe('pt-br');
  });

  test('returns undefined when nothing matches', () => {
    expect(match('ja-JP, *;q=0.1')).toBeUndefined();
    expect(match(null)).toBeUndefined();
  });

  test('keeps header order for equal weights', () => {
    expect(match('fr-CA, en-US')).toBe('fr');
    expect(match('en-US, fr-CA')).toBe('en');
  });

  test('drops q=0, malformed weights and empty parts', () => {
    expect(match('de;q=0, ,fr;q=abc, en;q=0.5')).toBe('en');
  });

  test('an empty header matches nothing', () => {
    expect(match('')).toBeUndefined();
  });
});
