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
  return { srcDir: join(packageDir, 'src'), distDir: join(dir, 'dist') };
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
    for (const path of ['llms.txt', 'spotify/setup.md', 'spotify/errors.md', 'govee/setup.md', 'govee/errors.md']) {
      expect(await Bun.file(join(options.distDir, path)).exists()).toBe(true);
      if (path !== 'llms.txt') expect(sitemap).toContain(`https://ai.itsbagelbot.com/${path}`);
      expect(index).toContain(`href="/${path}"`);
    }
    expect(sitemap).not.toContain('xmlns:ai');
    expect(sitemap).not.toContain('<ai:');
    expect(sitemap).not.toContain('llms.txt');
    expect(sitemap).not.toContain('<loc>https://ai.itsbagelbot.com/</loc>');
    for (const path of ['spotify/setup.md', 'govee/errors.md']) {
      const source = await readFile(join(options.srcDir, path), 'utf8');
      const updated = /^updated:\s*(\S+)$/m.exec(source)![1];
      expect(sitemap).toContain(`<loc>https://ai.itsbagelbot.com/${path}</loc><lastmod>${updated}</lastmod>`);
    }
    expect(await readFile(join(options.distDir, 'robots.txt'), 'utf8')).toContain('Sitemap: https://ai.itsbagelbot.com/sitemap.xml');
    expect(await readFile(join(options.distDir, '_headers'), 'utf8')).toContain('/*.md\n  Content-Type: text/markdown; charset=utf-8');
    expect(index).toContain('<meta name="robots" content="noindex, follow">');
    expect(index).toContain('<link rel="canonical" href="https://ai.itsbagelbot.com/">');
    expect(index).toContain('href="https://itsbagelbot.com/"');
  });

  test('emits canonical and noindex headers, the 404 page and the IndexNow key', async () => {
    const options = await fixture();
    await build(options);
    const headers = await readFile(join(options.distDir, '_headers'), 'utf8');
    expect(headers).toContain('/spotify/setup.md\n  Link: <https://ai.itsbagelbot.com/spotify/setup.md>; rel="canonical"\n');
    expect(headers).toContain('/llms.txt\n  Content-Type: text/plain; charset=utf-8\n  Cache-Control: public, max-age=300\n  X-Robots-Tag: noindex, follow\n');
    expect(headers).toContain('\n/\n  X-Robots-Tag: noindex, follow\n');
    expect(headers).not.toContain('llms.txt>; rel="canonical"');
    const notFound = await readFile(join(options.distDir, '404.html'), 'utf8');
    expect(notFound).toContain('noindex');
    expect(notFound).toContain('href="/"');
    expect(notFound).toContain('https://itsbagelbot.com/');
    const publicDir = join(packageDir, 'public');
    const names = await readdir(publicDir);
    expect(names.length).toBeGreaterThan(0);
    for (const name of names) {
      expect(await readFile(join(options.distDir, name), 'utf8')).toBe(await readFile(join(publicDir, name), 'utf8'));
    }
  });

  test('emits llms-full.txt with every page in order, noindex headers and no sitemap entry', async () => {
    const options = await fixture();
    await build(options);
    const full = await readFile(join(options.distDir, 'llms-full.txt'), 'utf8');
    expect(full.startsWith('# ItsBagelBot full reference for AI assistants\n')).toBe(true);
    const sources = [...full.matchAll(/^Source: (\S+)$/gm)].map((match) => match[1]);
    expect(new Set(sources).size).toBe(sources.length);
    expect(sources).not.toContain('https://ai.itsbagelbot.com/llms.txt');
    const pages = (await outputFiles(options.distDir)).map((file) => file.slice(options.distDir.length + 1)).filter((path) => path.endsWith('.md'));
    expect(sources.slice().sort()).toEqual(pages.map((path) => `https://ai.itsbagelbot.com/${path}`).sort());
    const rank = (path: string) => sources.indexOf(`https://ai.itsbagelbot.com/${path}`);
    expect(rank('product.md')).toBeLessThan(rank('migration.md'));
    expect(rank('migration.md')).toBeLessThan(rank('features/commands.md'));
    expect(rank('features/discord.md')).toBeLessThan(rank('setup/getting-started.md'));
    expect(rank('setup/import.md')).toBeLessThan(rank('errors/connection.md'));
    const updated = /^updated:\s*(\S+)$/m.exec(await readFile(join(options.srcDir, 'migration.md'), 'utf8'))![1];
    expect(full).toContain(`Source: https://ai.itsbagelbot.com/migration.md\nUpdated: ${updated}\n`);
    expect(await readFile(join(options.distDir, '_headers'), 'utf8')).toContain('/llms-full.txt\n  Content-Type: text/plain; charset=utf-8\n  Cache-Control: public, max-age=300\n  X-Robots-Tag: noindex, follow\n');
    expect(await readFile(join(options.distDir, 'sitemap.xml'), 'utf8')).not.toContain('llms-full.txt');
  });

  test('groups the llms.txt task list by kind', async () => {
    const options = await fixture();
    options.srcDir = join(options.distDir, '../src');
    await mkdir(options.srcDir, { recursive: true });
    const page = (title: string, extra: string) => `---\ntitle: ${title}\ndescription: Test\nupdated: 2026-09-01\n${extra}---\nBody\n`;
    await writeFile(join(options.srcDir, 'llms.txt.md'), page('Index', ''));
    await writeFile(join(options.srcDir, 'b.md'), page('Bee', 'kind: setup\n'));
    await writeFile(join(options.srcDir, 'a.md'), page('Ay', 'kind: setup\nhuman: https://itsbagelbot.com/a/\n'));
    await writeFile(join(options.srcDir, 'c.md'), page('Sea', ''));
    await build(options);
    const llms = await readFile(join(options.distDir, 'llms.txt'), 'utf8');
    expect(llms).toContain('## Setup\n\n- [Ay](https://ai.itsbagelbot.com/a.md): Test\n- [Bee](https://ai.itsbagelbot.com/b.md): Test\n\n## Tasks\n\n- [Sea](https://ai.itsbagelbot.com/c.md): Test\n');
    const sitemap = await readFile(join(options.distDir, 'sitemap.xml'), 'utf8');
    expect(sitemap).toContain('<lastmod>2026-09-01</lastmod>');
  });

  test('rejects a human link outside the marketing site', async () => {
    const options = await fixture();
    options.srcDir = join(options.distDir, '../src');
    await mkdir(options.srcDir, { recursive: true });
    await writeFile(join(options.srcDir, 'llms.txt.md'), '---\ntitle: Index\ndescription: Test\nupdated: 2026-09-01\n---\nBody\n');
    await writeFile(join(options.srcDir, 'bad.md'), '---\ntitle: Bad\ndescription: Test\nupdated: 2026-09-01\nhuman: https://example.com/\n---\nBody\n');
    await expect(build(options)).rejects.toThrow('bad.md: front matter human must start with https://itsbagelbot.com/');
  });

  test('has no em dash in any built output', async () => {
    const options = await fixture();
    await build(options);
    for (const file of await outputFiles(options.distDir)) {
      expect(await readFile(file, 'utf8')).not.toContain('\u2014');
    }
  });
});
