// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { SITE_NAV, resolveSiteLinks, type SiteLinkContext } from './site-links';

const context = (url?: string): SiteLinkContext => ({
  path: (path) => path,
  label: (key) => key,
  langQuery: '',
  url: url === undefined ? undefined : new URL(url),
});

const currentKeys = (url?: string) =>
  resolveSiteLinks(SITE_NAV, context(url))
    .filter((link) => link.current)
    .map((link) => link.label);

test.each([
  ['https://stats.itsbagelbot.com/', ['stats']],
  ['https://stats.itsbagelbot.com/?lang=fr', ['stats']],
  ['https://itsbagelbot.com/pricing/', ['pricing']],
  ['https://itsbagelbot.com/pricing/annual', []],
  ['https://dashboard.itsbagelbot.com/', []],
  ['https://status.itsbagelbot.com/incidents', []],
  [undefined, []],
] as const)('%s marks %j current', (url, want) => {
  expect(currentKeys(url)).toEqual([...want]);
});
