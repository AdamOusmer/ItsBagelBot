// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { Glob } from 'bun';
import { mock } from 'bun:test';
import { join } from 'node:path';

const LIB = join(import.meta.dir, '../src/lib');

export const privateEnv: Record<string, string | undefined> = {};

export function stubSvelteKit(options: { dev?: boolean } = {}): void {
  process.env.NEW_RELIC_ENABLED = 'false';
  process.env.LOG_LEVEL = 'silent';
  for (const file of new Glob('**/*.ts').scanSync(LIB)) {
    if (file.endsWith('.test.ts')) continue;
    const real = join(LIB, file);
    mock.module(`$lib/${file.slice(0, -'.ts'.length)}`, () => require(real));
  }
  mock.module('$app/environment', () => ({ browser: false, building: false, dev: options.dev ?? false }));
  mock.module('$env/dynamic/private', () => ({ env: privateEnv }));
}
