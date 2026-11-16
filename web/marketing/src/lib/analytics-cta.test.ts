// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { marketingPlacement } from './analytics-cta';

test.each([
  ['https://dashboard.itsbagelbot.com/auth/login?lang=fr', ['.hero'], '/', 'hero'],
  ['https://dashboard.itsbagelbot.com/auth/login', ['.bb-nav, .bb-mobile-menu'], '/vs/nightbot/', 'nav'],
  ['https://dashboard.itsbagelbot.com/billing?subscribe=1', ['.tiers'], '/pricing/', 'pricing'],
  ['https://dashboard.itsbagelbot.com/', ['.bb-footer'], '/', 'footer'],
  ['https://dashboard.itsbagelbot.com/', ['.play'], '/', 'playground'],
  ['https://dashboard.itsbagelbot.com/', ['.finale'], '/', 'finale'],
  ['https://dashboard.itsbagelbot.com/auth/login', [], '/guides/first-command/', 'guide'],
  ['https://dashboard.itsbagelbot.com/auth/login', [], '/fr/vs/', 'compare'],
  ['https://dashboard.itsbagelbot.com/auth/login', [], '/pt-br/import/', 'import'],
  ['https://dashboard.itsbagelbot.com/commands?private=config', ['.builder'], '/command-builder/', 'builder'],
  ['https://dashboard.itsbagelbot.com/?install=1', ['.bb-nav, .bb-mobile-menu'], '/', 'app'],
  ['https://dashboard.itsbagelbot.com/auth/login', [], '/timers/', 'landing'],
  ['https://dashboard.itsbagelbot.com.evil.example/auth/login', ['.hero'], '/', undefined],
  ['https://itsbagelbot.com/pricing/', [], '/', undefined],
  ['mailto:support@itsbagelbot.com', [], '/', undefined]
] as const)('classifies %s in %s on %s', (href, sections, pathname, expected) => {
  const link = { href, closest: (selector: string) => (sections as readonly string[]).includes(selector) ? {} : null };
  expect(marketingPlacement(link as unknown as HTMLAnchorElement, pathname)).toBe(expected);
});
