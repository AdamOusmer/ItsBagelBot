// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { mock } from 'bun:test';

export const privateEnv: Record<string, string | undefined> = {};

export function stubSvelteKit(options: { dev?: boolean } = {}): void {
  process.env.NEW_RELIC_ENABLED = 'false';
  process.env.LOG_LEVEL = 'silent';
  mock.module('$app/env', () => ({ browser: false, building: false, dev: options.dev ?? false }));
  mock.module('@bagel/kit/server/env', () => ({ env: privateEnv }));
}
