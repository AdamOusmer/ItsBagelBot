// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { scopeGap } from '../spotify';

mock.module('@bagel/kit', () => ({ scopeGap }));
mock.module('@bagel/kit/server/env', () => ({ env: {} }));

const { scopes } = await import('../../../dashboard/src/lib/server/oauth');
const source = await Bun.file(new URL('../../../../internal/domain/twitchauth/scopes.go', import.meta.url)).text();
const manifest = await Bun.file(new URL('../../../../deploy/k8s/console-dashboard.yaml', import.meta.url)).text();
const declaration = source.match(/var RequiredBroadcasterScopes = \[\]string\{([^}]*)\}/)?.[1] ?? '';
const goScopes = [...declaration.matchAll(/"([^"]+)"/g)].map((match) => match[1]!).sort();

test.each([
  {
    name: 'dashboard defaults match Go broadcaster scopes',
    actual: scopes().filter((scope) => scope !== 'openid').sort(),
    expected: goScopes
  },
  {
    name: 'dashboard deployment uses the defaults',
    actual: manifest.match(/\bDASHBOARD_LOGIN_SCOPES\b/g) ?? [],
    expected: []
  }
])('$name', ({ actual, expected }) => {
  expect(actual).toEqual(expected);
});
