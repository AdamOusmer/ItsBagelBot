// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { SITE } from '@bagel/kit/site-links';

test.each(['../../public/_headers', '../layouts/Layout.astro'])('allows the beacon origin only as a connection in %s', (file) => {
  const source = readFileSync(new URL(file, import.meta.url), 'utf8');
  const connect = source.match(/connect-src [^;\n`$]+/)?.[0].split(' ');
  expect(connect).toContain(SITE.dashboard);
});
