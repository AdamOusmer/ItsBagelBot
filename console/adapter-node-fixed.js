import adapterNode from '@sveltejs/adapter-node';
import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';

const BAD_DIR = 'const dir = path.dirname(fileURLToPath(import.meta.url));';
const BUILD_ROOT_DIR =
  "const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');";

function patchRuntimeAssetDir(out) {
  const chunksDir = path.join(out, 'server', 'chunks');
  if (!existsSync(chunksDir)) return 0;

  let patched = 0;
  for (const entry of readdirSync(chunksDir, { withFileTypes: true })) {
    if (!entry.isFile() || !entry.name.endsWith('.js')) continue;

    const file = path.join(chunksDir, entry.name);
    const source = readFileSync(file, 'utf8');
    if (!source.includes(BAD_DIR)) continue;

    writeFileSync(file, source.replace(BAD_DIR, BUILD_ROOT_DIR));
    patched += 1;
  }

  return patched;
}

export default function adapter(options = {}) {
  const upstream = adapterNode(options);
  const out = options.out ?? 'build';

  return {
    ...upstream,
    async adapt(builder) {
      await upstream.adapt(builder);

      const patched = patchRuntimeAssetDir(out);
      if (patched > 0) {
        builder.log.minor(`Patched adapter-node asset root in ${patched} runtime chunk(s)`);
      }
    }
  };
}
