// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { marketingPlacement } from './analytics-cta';

test.each([
  ['https://dashboard.itsbagelbot.com/auth/login?lang=fr', ['.hero'], 'hero'],
  ['https://dashboard.itsbagelbot.com/auth/login', ['.bb-nav, .bb-mobile-menu'], 'nav'],
  ['https://dashboard.itsbagelbot.com/billing?subscribe=1', ['.tiers'], 'pricing'],
  ['https://dashboard.itsbagelbot.com/', ['.bb-footer'], 'footer'],
  ['https://dashboard.itsbagelbot.com/', ['.play'], 'playground'],
  ['https://dashboard.itsbagelbot.com/', ['.finale'], 'finale'],
  ['https://dashboard.itsbagelbot.com/', ['[data-analytics-cta="guide"]'], 'guide'],
  ['https://dashboard.itsbagelbot.com/', ['[data-analytics-cta="compare"]'], 'compare'],
  ['https://dashboard.itsbagelbot.com/', ['[data-analytics-cta="import"]'], 'import'],
  ['https://dashboard.itsbagelbot.com/commands?private=config', ['.builder'], 'builder'],
  ['https://dashboard.itsbagelbot.com/?install=1', ['[data-analytics-cta="app"]', '.bb-nav, .bb-mobile-menu'], 'app'],
  ['https://dashboard.itsbagelbot.com/auth/login', [], 'landing'],
  ['https://dashboard.itsbagelbot.com.evil.example/auth/login', ['.hero'], undefined],
  ['https://itsbagelbot.com/pricing/', [], undefined],
  ['mailto:support@itsbagelbot.com', [], undefined]
] as const)('classifies %s by placement %s', (href, selectors, expected) => {
  const link = { href, closest: (selector: string) => (selectors as readonly string[]).includes(selector) ? {} : null };
  expect(marketingPlacement(link as HTMLAnchorElement)).toBe(expected);
});
