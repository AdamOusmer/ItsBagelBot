// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fileURLToPath } from 'node:url';
import { assertProductionClean } from '../../kit/scripts/assert-production-clean';

await assertProductionClean({
  app: 'dashboard',
  buildRoot: fileURLToPath(new URL('../build/', import.meta.url)),
  // `demo(?!-checkout)` is lib/flow/demo.ts; the billing demo-checkout route directory must not match.
  fixtureChunks: ['demo-data', 'demo-notifications', 'demo(?!-checkout)', 'sample'],
  // Verbatim from billing/demo-checkout/copy/en.json; drift makes this check pass vacuously.
  demoCopy: ['No card is charged', 'Demo checkout',
    // The Spotify sample-key shortcut must be erased, including source maps.
    'preview-client-id',
    // Verbatim from lib/flow/demo.ts fixtures (the Recipes pages themselves ship); drift makes this check pass vacuously.
    'Hugs someone, most of the time', 'Hydrate (demo reward)', '#announcements (demo channel)', 'Sunset glow (demo scene)']
});
