import { afterEach, describe, expect, test } from 'bun:test';
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { build } from './build';

const packageDir = resolve(import.meta.dir, '..');
const temporaryDirs: string[] = [];

async function fixture() {
  const dir = await mkdtemp(join(tmpdir(), 'bagel-ai-docs-'));
  temporaryDirs.push(dir);
  return { srcDir: join(packageDir, 'src'), distDir: join(dir, 'dist'), date: '2026-10-12' };
}

async function outputFiles(dir: string): Promise<string[]> {
  const paths: string[] = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) paths.push(...await outputFiles(path));
    else paths.push(path);
  }
  return paths;
}

afterEach(async () => {
  await Promise.all(temporaryDirs.splice(0).map((dir) => rm(dir, { recursive: true, force: true })));
});

describe('AI documentation build', () => {
  test('builds the real templates and resolves every placeholder', async () => {
    const options = await fixture();
    await build(options);
    for (const file of await outputFiles(options.distDir)) {
      expect(await readFile(file, 'utf8')).not.toContain('{{');
    }
    const setup = await readFile(join(options.distDir, 'spotify/setup.md'), 'utf8');
    const spotifySetup = await Bun.file(resolve(packageDir, '../../locales/en/console/spotifySetup.json')).json();
    expect(setup).toContain(spotifySetup.redirectLabel);
    const errors = await readFile(join(options.distDir, 'spotify/errors.md'), 'utf8');
    const spotifyErrors = await Bun.file(resolve(packageDir, '../../locales/en/console/spotify/errors.json')).json();
    const songqueue = await Bun.file(resolve(packageDir, '../../locales/en/chat/songqueue.json')).json();
    expect(errors).toContain(spotifyErrors.declined);
    expect(errors).toContain(songqueue['err.setup']);
  });

  test('reports the template file, line and key for an unknown placeholder', async () => {
    const options = await fixture();
    options.srcDir = join(options.distDir, '../src');
    await mkdir(options.srcDir, { recursive: true });
    await writeFile(join(options.srcDir, 'unknown.md'), '---\ntitle: {{console:common.unit.label}}\ndescription: Test\nupdated: 2026-10-12\n---\n{{console:spotifySetup.unknownKey}}\n');
    await expect(build(options)).rejects.toThrow('unknown.md:6: unresolved placeholder {{console:spotifySetup.unknownKey}}');
  });

  test('writes every page and the site discovery files', async () => {
    const options = await fixture();
    await build(options);
    const sitemap = await readFile(join(options.distDir, 'sitemap.xml'), 'utf8');
    const index = await readFile(join(options.distDir, 'index.html'), 'utf8');
    expect(sitemap).toContain('<loc>https://ai.itsbagelbot.com/</loc>');
    for (const path of ['llms.txt', 'spotify/setup.md', 'spotify/errors.md', 'govee/setup.md', 'govee/errors.md']) {
      expect(await Bun.file(join(options.distDir, path)).exists()).toBe(true);
      expect(sitemap).toContain(`https://ai.itsbagelbot.com/${path}`);
      expect(index).toContain(`href="/${path}"`);
    }
    expect(sitemap).toContain('<lastmod>2026-10-12</lastmod>');
    expect(await readFile(join(options.distDir, 'robots.txt'), 'utf8')).toContain('Sitemap: https://ai.itsbagelbot.com/sitemap.xml');
    expect(await readFile(join(options.distDir, '_headers'), 'utf8')).toContain('/*.md\n  Content-Type: text/markdown; charset=utf-8');
  });

  test('has no em dash in any built output', async () => {
    const options = await fixture();
    await build(options);
    for (const file of await outputFiles(options.distDir)) {
      expect(await readFile(file, 'utf8')).not.toContain('\u2014');
    }
  });
});
