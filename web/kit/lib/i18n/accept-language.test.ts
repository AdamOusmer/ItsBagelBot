// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { acceptedLanguages } from './accept-language';

describe('acceptedLanguages', () => {
  test('orders by q-weight, not header order', () => {
    expect(acceptedLanguages('fr;q=0.8, en-GB')).toEqual(['en', 'fr']);
  });

  test('keeps header order for equal weights', () => {
    expect(acceptedLanguages('fr-CA, en-US')).toEqual(['fr', 'en']);
  });

  test('drops q=0, malformed weights and empty parts', () => {
    expect(acceptedLanguages('de;q=0, ,fr;q=abc, en;q=0.5')).toEqual(['en']);
  });

  test('handles a missing header', () => {
    expect(acceptedLanguages(null)).toEqual([]);
    expect(acceptedLanguages('')).toEqual([]);
  });
});
