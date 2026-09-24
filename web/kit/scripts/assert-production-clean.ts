// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdir, readFile } from 'node:fs/promises';
import { extname, relative, sep } from 'node:path';

const textExtensions = new Set(['.css', '.html', '.js', '.json', '.map']);

export type ProductionCleanSpec = {
  app: string;
  buildRoot: string;
  fixtureChunks: string[];
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

async function scanFile(
  file: string,
  name: string,
  chunks: RegExp,
  forbidden: string[]
): Promise<string[]> {
  const emittedChunk = chunks.test(name) ? [`${name}: development fixture chunk was emitted`] : [];
  if (!textExtensions.has(extname(file))) return emittedChunk;

  const body = await readFile(file, 'utf8');
  const referenced = forbidden
    .filter((token) => body.includes(token))
    .map((token) => `${name}: references development fixture module ${JSON.stringify(token)}`);
  return [...emittedChunk, ...referenced];
}

export async function assertProductionClean(spec: ProductionCleanSpec): Promise<void> {
  const forbidden = [
    `${spec.app.toUpperCase()}_DEV_FIXTURE_INCLUDED_IN_PRODUCTION`,
    'demo-data',
    'env.DEMO',
    ...(spec.demoCopy ?? [])
  ];
  const chunks = chunkPattern(spec.fixtureChunks);
  const files = await filesUnder(spec.buildRoot);

  const failures = (
    await Promise.all(
      files.map((file) => scanFile(file, relative(spec.buildRoot, file), chunks, forbidden))
    )
  ).flat();

  if (failures.length > 0) {
    console.error(`Production ${spec.app} build contains development-only demo artifacts:`);
    for (const failure of failures) console.error(`- ${failure}`);
    process.exit(1);
  }

  console.log(
    `Verified ${files.length} production build files: no ${spec.app} demo fixtures emitted.`
  );
}
