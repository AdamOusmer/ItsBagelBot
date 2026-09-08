// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Dashboard's output-layer demo gate. The scan lives in
// ../../shared/scripts/assert-production-clean.ts; this names what is
// dashboard-specific about it.
import { fileURLToPath } from 'node:url';
import { assertProductionClean } from '../../shared/scripts/assert-production-clean';

await assertProductionClean({
  app: 'dashboard',
  buildRoot: fileURLToPath(new URL('../build/', import.meta.url)),
  fixtureChunks: ['demo-data', 'demo-notifications', 'sample'],
  // Copy from the fake checkout page. Its server side 404s outside a demo
  // build, but the component itself was still compiled and its markup shipped
  // to browsers, so `vite.config.ts` now replaces the module. These strings are
  // what proves that stayed true: they can only be here if it did not.
  demoCopy: ['No card is charged', 'Demo checkout']
});
