// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fileURLToPath } from 'node:url';
import { assertProductionClean } from '../../kit/scripts/assert-production-clean';

await assertProductionClean({
  app: 'admin',
  buildRoot: fileURLToPath(new URL('../build/', import.meta.url)),
  fixtureChunks: ['demo-data', 'demo-access', 'sample']
});
