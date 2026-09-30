// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { acceptedLanguages, matchAcceptLanguage } from './accept-language';

describe('acceptedLanguages', () => {
  test('orders by q-weight, not header order', () => {
    expect(acceptedLanguages('fr;q=0.8, en-GB')).toEqual(['en-gb', 'fr']);
  });

  test('keeps header order for equal weights', () => {
    expect(acceptedLanguages('fr-CA, en-US')).toEqual(['fr-ca', 'en-us']);
  });

  test('drops q=0, malformed weights and empty parts', () => {
    expect(acceptedLanguages('de;q=0, ,fr;q=abc, en;q=0.5')).toEqual(['en']);
  });

  test('handles a missing header', () => {
    expect(acceptedLanguages(null)).toEqual([]);
    expect(acceptedLanguages('')).toEqual([]);
  });
});

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
});
