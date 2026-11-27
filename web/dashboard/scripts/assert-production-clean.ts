// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fileURLToPath } from 'node:url';
import { assertProductionClean } from '../../kit/scripts/assert-production-clean';

await assertProductionClean({
  app: 'dashboard',
  buildRoot: fileURLToPath(new URL('../build/', import.meta.url)),
  fixtureChunks: ['fixtures', 'flow-fixtures', 'import-fixtures', 'backend'],
  // Verbatim from src/dev and billing/demo-checkout/copy/en.json; drift makes this check pass vacuously.
  demoCopy: ['No card is charged', 'Demo checkout', 'demo-session', 'crumbcatcher', 'Hugs someone, most of the time',
    '(demo reward)', '(demo scene)']
});
