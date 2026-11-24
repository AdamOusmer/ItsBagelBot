// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { join } from 'node:path';

const here = import.meta.dir;
export const outDir = join(here, '../../../app/controls/gateway/priv/overlay');
export const targets = { 'runtime.js': 'entry.ts', 'frame.js': 'frame.ts' } as const;
const banner = '/* Copyright (c) 2026 Adam Ousmer. All rights reserved. Proprietary. No license granted. See LICENSE.md. */';

export async function bundle(entry: string): Promise<string> {
  const result = await Bun.build({ entrypoints: [join(here, entry)], target: 'browser', format: 'iife', minify: true, banner });
  if (!result.success) throw new Error(result.logs.join('\n'));
  return await result.outputs[0]!.text();
}

if (import.meta.main) {
  for (const [name, entry] of Object.entries(targets)) await Bun.write(join(outDir, name), await bundle(entry));
}
