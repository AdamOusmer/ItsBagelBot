import { readdir, readFile } from 'node:fs/promises';
import { extname, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const buildRoot = fileURLToPath(new URL('../build/', import.meta.url));
const textExtensions = new Set(['.css', '.html', '.js', '.json', '.map']);

// Every seeded fixture lives behind the guarded demo-data module. Detect the
// module itself instead of maintaining an inevitably incomplete list of its
// current values: an emitted chunk, source-map reference, import edge, or the
// module's side-effect sentinel is enough to fail the production build.
const forbiddenModuleTokens = [
  'ADMIN_DEV_FIXTURE_INCLUDED_IN_PRODUCTION',
  'demo-data'
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
  if (/(^|[/\\])(demo-data|demo-access|sample)([.-]|[/\\])/.test(name)) {
    failures.push(`${name}: development fixture chunk was emitted`);
  }
  if (!textExtensions.has(extname(file))) continue;

  const body = await readFile(file, 'utf8');
  for (const token of forbiddenModuleTokens) {
    if (body.includes(token)) failures.push(`${name}: references development fixture module ${JSON.stringify(token)}`);
  }
}

if (failures.length > 0) {
  console.error('Production admin build contains development-only demo artifacts:');
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(`Verified ${files.length} production build files: no admin demo fixtures emitted.`);
