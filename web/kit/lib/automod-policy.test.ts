// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { AUTOMOD_MODULE } from './catalog/automod';
import {
  AUTOMOD_ACCOUNT_KEY,
  AUTOMOD_DOMAIN_KEYS,
  AUTOMOD_TERM_KEYS,
  automodConfigIssue,
  splitAutomodList,
  validAutomodAccount,
  validAutomodDomain
} from './automod-policy';

describe('automod policy', () => {
  test('every validated key is a dashboard field', () => {
    const fields = new Set((AUTOMOD_MODULE.settings ?? []).map((f) => f.key));
    for (const key of [...AUTOMOD_TERM_KEYS, ...AUTOMOD_DOMAIN_KEYS, AUTOMOD_ACCOUNT_KEY]) expect(fields.has(key)).toBe(true);
  });

  test('lists split on commas and new lines like Rust', () => {
    expect(splitAutomodList(' a, b\n\nc ,')).toEqual(['a', 'b', 'c']);
    expect(splitAutomodList(undefined)).toEqual([]);
  });

  test.each([
    ['example.com', true],
    ['sub.EXAMPLE.com', true],
    ['grabify.link', true],
    ['1.2.3.4', true],
    ['[::1]', true],
    ['bücher.de', true],
    ['https://example.com', false],
    ['example.com/path', false],
    ['example.com:8080', false],
    ['example.com.', false],
    ['-bad.com', false],
    ['user@example.com', false],
    ['exa mple.com', false],
    ['[::1', false],
    ['', false]
  ])('domain %p is %p', (value, ok) => {
    expect(validAutomodDomain(value)).toBe(ok);
  });

  test.each([
    ['123456789', true],
    ['0', false],
    ['0123', false],
    ['someuser', false],
    ['123456789012345678901', false]
  ])('account %p is %p', (value, ok) => {
    expect(validAutomodAccount(value)).toBe(ok);
  });

  test('a clean config compiles', () => {
    expect(
      automodConfigIssue({
        block_terms: 'one, two',
        nsfw_terms: 'adult',
        blocked_domains: 'example.com',
        ip_logger_domains: 'grabify.link',
        blocked_accounts: '111',
        block_links: 'on'
      })
    ).toBeNull();
  });

  test('names the first entry Rust would reject', () => {
    expect(automodConfigIssue({ blocked_domains: 'ok.com, https://bad.com' })).toEqual({
      code: 'domain',
      field: 'blocked_domains',
      entry: 'https://bad.com'
    });
    expect(automodConfigIssue({ blocked_accounts: 'someuser' })?.code).toBe('account');
    expect(automodConfigIssue({ slur_terms: 'x'.repeat(129) })?.code).toBe('term');
    expect(automodConfigIssue({ slur_terms: '​' })?.code).toBe('term');
  });

  test('enforces the shared 256-rule budgets', () => {
    const terms = (n: number) => Array.from({ length: n }, (_, i) => `t${i}`).join(',');
    expect(automodConfigIssue({ block_terms: terms(200), nsfw_terms: terms(56) })).toBeNull();
    expect(automodConfigIssue({ block_terms: terms(200), nsfw_terms: terms(57) })?.code).toBe('tooMany');
    const ids = (n: number) => Array.from({ length: n }, (_, i) => String(i + 1)).join(',');
    expect(automodConfigIssue({ blocked_accounts: ids(256) })).toBeNull();
    expect(automodConfigIssue({ blocked_accounts: ids(200), blocked_domains: terms(57).replaceAll(',', '.com,') + '.com' })?.code).toBe('tooMany');
  });

  test('refuses configs over the modules 16 KiB limit', () => {
    expect(automodConfigIssue({ block_terms: 'a'.repeat(17 * 1024) })?.code).toBe('tooLarge');
  });
});
