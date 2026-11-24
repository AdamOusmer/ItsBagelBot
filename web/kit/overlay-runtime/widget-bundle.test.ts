// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { MAX_BUNDLE_BYTES, fieldValues, parseBundle } from './bundle';
import type { CustomField } from './widget-types';

const field: CustomField = { key: 'title', label: 'Title', type: 'text', default: 'Hello' };
const bundle = (over: object = {}): string => JSON.stringify({ html: '<p></p>', css: 'p{}', js: '', fields: [field], ...over });

describe('parseBundle', () => {
  test('reads html, css, js and fields', () => {
    expect(parseBundle(bundle())).toEqual({ html: '<p></p>', css: 'p{}', js: '', fields: [field] });
  });

  test.each([
    ['an oversized bundle', JSON.stringify({ html: 'a'.repeat(MAX_BUNDLE_BYTES), css: '', js: '' })],
    ['a missing js string', JSON.stringify({ html: '', css: '' })],
    ['a non-object body', '[]'],
    ['an unknown field type', bundle({ fields: [{ ...field, type: 'script' }] })],
    ['duplicate field keys', bundle({ fields: [field, field] })]
  ])('rejects %s', (_name, text) => {
    expect(() => parseBundle(text)).toThrow();
  });
});

describe('fieldValues', () => {
  test.each([
    ['uses the default without a value', {}, 'Hello'],
    ['uses the supplied value', { title: 'Hi' }, 'Hi']
  ])('%s', (_name, values, want) => {
    expect(fieldValues([field], values)).toEqual({ title: want });
  });

  test('rejects a value of the wrong type', () => {
    expect(() => fieldValues([field], { title: 4 })).toThrow();
  });
});
