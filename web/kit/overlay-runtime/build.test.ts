// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { bundle, outDir, targets } from './build';

test.each(Object.entries(targets))('committed %s is built from the current source', async (name, entry) => {
  const committed = readFileSync(join(outDir, name), 'utf8');
  if ((await bundle(entry)) !== committed) throw new Error(`${name} is stale: run bun web/kit/overlay-runtime/build.ts`);
});
