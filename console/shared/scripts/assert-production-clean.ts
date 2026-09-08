// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Output-layer demo gate: scan an app's emitted production build for anything
 * that could only be there if a demo branch survived. Third of the three layers
 * (source: assert-demo-gated.mjs, build: strip-demo-routes.mjs, output: this).
 *
 * Both consoles ran a byte-for-byte copy of this walk, and they had already
 * drifted: only the dashboard's copy scanned for `env.DEMO`, so an ungated
 * env read in the admin bundle would have passed its own build. The scan is
 * the same question for every app; what differs is the sentinel, the fixture
 * chunk names, and the app's own demo copy. Those are the parameters.
 *
 * Detects the fixture MODULE rather than a list of its current values: an
 * emitted chunk, a source-map reference, an import edge, the module's
 * side-effect sentinel, or a surviving DEMO env read is each enough to fail
 * the build. A list of fixture values would be incomplete the day someone adds
 * one.
 *
 * A surviving `env.DEMO` is failed for both apps because it means a branch
 * escaped Rollup's dead-code elimination, which is exactly the condition that
 * would let a runtime env var re-enable demo mode on a shipped image. The
 * deliberate boot-time refusal reads the key by lookup through
 * shared/lib/server/demo-guard.ts, which never spells it this way.
 */

import { readdir, readFile } from 'node:fs/promises';
import { extname, relative, sep } from 'node:path';

const textExtensions = new Set(['.css', '.html', '.js', '.json', '.map']);

export type ProductionCleanSpec = {
  /** App name, for the messages. */
  app: string;
  /** Absolute path of the app's build/ directory. */
  buildRoot: string;
  /**
   * Fixture chunk basenames that must never be emitted, e.g. ['demo-data',
   * 'sample']. Matched as whole path segments, not substrings.
   */
  fixtureChunks: string[];
  /**
   * Strings from demo-only COPY that a stripped route must not have shipped.
   * Route stripping (vite.config.ts) removes markup a gate cannot; these
   * prove it stayed removed.
   */
  demoCopy?: string[];
};

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

function chunkPattern(fixtureChunks: string[]): RegExp {
  return new RegExp(`(^|[/\\\\])(${fixtureChunks.join('|')})([.-]|[/\\\\])`);
}

export async function assertProductionClean(spec: ProductionCleanSpec): Promise<void> {
  const forbidden = [
    `${spec.app.toUpperCase()}_DEV_FIXTURE_INCLUDED_IN_PRODUCTION`,
    'demo-data',
    'env.DEMO',
    ...(spec.demoCopy ?? [])
  ];
  const chunks = chunkPattern(spec.fixtureChunks);

  const failures: string[] = [];
  const files = await filesUnder(spec.buildRoot);

  for (const file of files) {
    const name = relative(spec.buildRoot, file);
    if (chunks.test(name)) failures.push(`${name}: development fixture chunk was emitted`);
    if (!textExtensions.has(extname(file))) continue;

    const body = await readFile(file, 'utf8');
    for (const token of forbidden) {
      if (body.includes(token)) {
        failures.push(`${name}: references development fixture module ${JSON.stringify(token)}`);
      }
    }
  }

  if (failures.length > 0) {
    console.error(`Production ${spec.app} build contains development-only demo artifacts:`);
    for (const failure of failures) console.error(`- ${failure}`);
    process.exit(1);
  }

  console.log(
    `Verified ${files.length} production build files: no ${spec.app} demo fixtures emitted.`
  );
}
