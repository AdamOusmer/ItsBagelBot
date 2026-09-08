// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Admin's output-layer demo gate. The scan lives in
// ../../shared/scripts/assert-production-clean.ts; this names what is
// admin-specific about it. Kept as a per-app entry rather than one script with
// an app argument so the fixture chunks and demo copy are declared next to the
// app that owns them, and a new demo surface is added here rather than in a
// shared list of everyone's strings.
import { fileURLToPath } from 'node:url';
import { assertProductionClean } from '../../shared/scripts/assert-production-clean';

await assertProductionClean({
  app: 'admin',
  buildRoot: fileURLToPath(new URL('../build/', import.meta.url)),
  fixtureChunks: ['demo-data', 'demo-access', 'sample']
});
