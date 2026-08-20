// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdir, readFile } from 'node:fs/promises';
import { extname, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const buildRoot = fileURLToPath(new URL('../build/', import.meta.url));
const textExtensions = new Set(['.css', '.html', '.js', '.json', '.map']);

// Every seeded fixture lives behind the guarded demo-data module, and every
// demo branch is gated on the build-time `dev` constant. Detect the module and
// the env read instead of maintaining an inevitably incomplete list of fixture
// values: an emitted chunk, source-map reference, import edge, the module's
// side-effect sentinel, or a surviving DEMO env read is enough to fail the
// production build. A surviving `env.DEMO` also means a branch escaped Rollup's
// dead-code elimination, which is exactly the condition that would let a
// runtime env var re-enable demo mode on a shipped image.
const forbiddenModuleTokens = [
  'DASHBOARD_DEV_FIXTURE_INCLUDED_IN_PRODUCTION',
  'demo-data',
  'env.DEMO',
  // Copy from the fake checkout page. Its server side 404s outside a demo
  // build, but the component itself was still compiled and its markup shipped
  // to browsers, so `vite.config.ts` now replaces the module. These strings
  // are what proves that stayed true: they can only be here if it did not.
  'No card is charged',
  'Demo checkout'
] as const;

async function filesUnder(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true });
  const nested = await Promise.all(
    entries.map((entry) => {
      const path = `${dir}${sep}${entry.name}`;
      return entry.isDirectory() ? filesUnder(path) : Promise.resolve([path]);
    })
  );
  return nested.flat();
}

const failures: string[] = [];
const files = await filesUnder(buildRoot);

for (const file of files) {
  const name = relative(buildRoot, file);
  if (/(^|[/\\])(demo-data|demo-notifications|sample)([.-]|[/\\])/.test(name)) {
    failures.push(`${name}: development fixture chunk was emitted`);
  }
  if (!textExtensions.has(extname(file))) continue;

  const body = await readFile(file, 'utf8');
  for (const token of forbiddenModuleTokens) {
    if (body.includes(token)) failures.push(`${name}: references development fixture module ${JSON.stringify(token)}`);
  }
}

if (failures.length > 0) {
  console.error('Production dashboard build contains development-only demo artifacts:');
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(`Verified ${files.length} production build files: no dashboard demo fixtures emitted.`);
