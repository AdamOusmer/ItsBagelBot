// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { decodeModuleFields } from './module-projection';

test('module reads retain revision and account metadata for fenced replacement', () => {
  const decoded = decodeModuleFields({
    'modules:projected': '1',
    'module:queue:enabled': '1', 'module:queue:config': '{}', 'module:queue:revision': '8',
    'module:loyalty:enabled': '0', 'module:loyalty:config': '{}',
    'module:loyalty:revision': '4', 'module:loyalty:account_created_at': '1790481600000000'
  });
  expect(decoded.projected).toBe(true);
  expect(decoded.modules).toEqual([
    { name: 'queue', is_enabled: true, configs: {}, revision: 8 },
    { name: 'loyalty', is_enabled: false, configs: {}, revision: 4, account_created_at: 1790481600000000 }
  ]);
});

test('legacy module fields can still be read and empty snapshots stay empty', () => {
  expect(decodeModuleFields({ 'module:queue:enabled': '0' }).modules)
    .toEqual([{ name: 'queue', is_enabled: false }]);
  expect(decodeModuleFields({ 'modules:projected': '1' })).toEqual({ modules: [], projected: true });
  expect(decodeModuleFields({})).toEqual({ modules: [], projected: false });
});
