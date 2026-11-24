// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { TERMS_VERSION } from './consent';

const source = await Bun.file(new URL('../../../internal/domain/consent/consent.go', import.meta.url)).text();

test('the dashboard uses the Users terms version', () => {
  expect(TERMS_VERSION).toBe(source.match(/^\s*TermsVersion\s*=\s*"(\d{4}-\d{2})"/m)?.[1]);
});
