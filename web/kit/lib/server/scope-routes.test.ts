// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { readdir } from 'node:fs/promises';

process.env.NEW_RELIC_ENABLED = 'false';
mock.module('$app/env', () => ({ dev: false }));

const { SCOPES: dashboard } = await import('../../../dashboard/src/lib/server/services/fabric');
const { SCOPES: admin } = await import('../../../admin/src/lib/server/services');
const scopeDir = new URL('../../../../internal/domain/invalidate/', import.meta.url);
const files = (await readdir(scopeDir)).filter((name) => /^scopes.*\.go$/.test(name));
const sources = await Promise.all(files.map((name) => Bun.file(new URL(name, scopeDir)).text()));
const source = sources.join('\n');
const goScopes = [...source.matchAll(/Scope\w+\s+Scope\s*=\s*"([^"]+)"/g)].map((match) => match[1]!);

test.each([
  { app: 'dashboard', scopes: dashboard, local: [] },
  { app: 'admin', scopes: admin, local: ['staff', 'audit'] }
])('$app explicitly routes every Go scope and its own broadcasts', ({ scopes, local }) => {
  expect(Object.keys(scopes).sort()).toEqual([...goScopes, ...local, '*'].sort());
});
