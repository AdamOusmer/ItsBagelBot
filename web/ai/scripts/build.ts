import { mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { dirname, join, relative, resolve } from 'node:path';

const packageDir = resolve(import.meta.dir, '..');
const origin = 'https://ai.itsbagelbot.com';

type BuildOptions = {
  srcDir?: string;
  distDir?: string;
  localeDir?: string;
  date?: string;
};

type Template = { path: string; text: string };
type Page = { path: string; title: string; description: string; updated: string; body: string };
type CatalogCache = Map<string, unknown>;
type PlaceholderOptions = { localeDir: string; cache: CatalogCache };
type CatalogReference = { surface: string; keys: string[] };
type SiteOutput = { distDir: string; index: Page; tasks: Page[] };

async function files(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true });
  const paths: string[] = [];
  for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name, 'en'))) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) paths.push(...await files(path));
    else if (entry.isFile()) paths.push(path);
  }
  return paths;
}

function validIsoDay(value: string, date: Date): boolean {
  return /^\d{4}-\d{2}-\d{2}$/.test(value) && Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value;
}

function dateString(value: string): string {
  const date = new Date(`${value}T00:00:00Z`);
  if (!validIsoDay(value, date)) throw new Error(`Invalid build date: ${value}`);
  return value;
}

function validEpoch(raw: string, seconds: number, date: Date): boolean {
  return Number.isInteger(seconds) && Number.isFinite(date.getTime()) && raw.trim() !== '';
}

function buildDate(options: BuildOptions): string | undefined {
  if (options.date !== undefined) return dateString(options.date);
  const raw = process.env.SOURCE_DATE_EPOCH;
  if (raw === undefined) return undefined;
  const seconds = Number(raw);
  const date = new Date(seconds * 1000);
  if (!validEpoch(raw, seconds, date)) throw new Error(`Invalid SOURCE_DATE_EPOCH: ${raw}`);
  return date.toISOString().slice(0, 10);
}

function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&apos;' }[character]!));
}

function missingCatalog(error: unknown): undefined {
  if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
  return undefined;
}

async function loadCatalog(cache: CatalogCache, path: string): Promise<unknown> {
  if (!cache.has(path)) cache.set(path, await readFile(path, 'utf8').then(JSON.parse).catch(missingCatalog));
  return cache.get(path);
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function lookup(catalog: unknown, keys: string[]): unknown {
  if (!keys.length) return catalog;
  if (!isPlainObject(catalog)) return undefined;
  const key = keys.join('.');
  if (Object.hasOwn(catalog, key)) return catalog[key];
  return lookup(catalog[keys[0]], keys.slice(1));
}

async function lookupReference(reference: CatalogReference, options: PlaceholderOptions, end = reference.keys.length - 1): Promise<unknown> {
  if (end === 0) return undefined;
  const path = join(options.localeDir, reference.surface, ...reference.keys.slice(0, end)) + '.json';
  const catalog = await loadCatalog(options.cache, path);
  if (catalog === undefined) return lookupReference(reference, options, end - 1);
  return lookup(catalog, reference.keys.slice(end));
}

async function placeholderValue(match: RegExpExecArray, template: Template, options: PlaceholderOptions): Promise<string> {
  const reference = /^(console|chat|website):(.+)$/.exec(match[1].trim());
  const value = reference ? await lookupReference({ surface: reference[1], keys: reference[2].split('.') }, options) : undefined;
  if (typeof value !== 'string') {
    const line = template.text.slice(0, match.index).split('\n').length;
    throw new Error(`${template.path}:${line}: unresolved placeholder ${match[0]}`);
  }
  return value;
}

async function resolvePlaceholders(template: Template, options: PlaceholderOptions): Promise<string> {
  let text = '';
  let cursor = 0;
  for (const match of template.text.matchAll(/\{\{([^{}]+)\}\}/g)) {
    text += template.text.slice(cursor, match.index) + await placeholderValue(match, template, options);
    cursor = match.index + match[0].length;
  }
  return text + template.text.slice(cursor);
}

function parseFrontMatter(lines: string[]): Record<string, string> {
  const metadata: Record<string, string> = {};
  const fields = lines.map((line) => /^(title|description|updated):\s*(.*)$/.exec(line));
  for (const field of fields.filter((field): field is RegExpExecArray => field !== null)) {
    metadata[field[1]] = field[2].replace(/^(['"])(.*)\1$/, '$2');
  }
  return metadata;
}

function parsePage(template: Template, options: BuildOptions): Page {
  const frontMatter = /^---\n([\s\S]*?)\n---(?:\n|$)/.exec(template.text);
  if (!frontMatter) throw new Error(`${template.path}: missing front matter`);
  const metadata = parseFrontMatter(frontMatter[1].split('\n'));
  const missing = ['title', 'description', 'updated'].find((field) => !metadata[field]);
  if (missing) throw new Error(`${template.path}: missing front matter ${missing}`);
  return {
    path: template.path === 'llms.txt.md' ? 'llms.txt' : template.path,
    title: metadata.title,
    description: metadata.description,
    updated: options.date ?? dateString(metadata.updated),
    body: template.text.slice(frontMatter[0].length).trim(),
  };
}

function renderPage(page: Page, index: Page, tasks: Page[]): string {
  const markdown = `# ${page.title}\n\n${page.body}`.trimEnd() + '\n';
  if (page !== index) return markdown;
  return markdown + '\n' + tasks.map((task) => `- [${task.title}](${origin}/${task.path}): ${task.description}`).join('\n') + '\n';
}

async function writePage(page: Page, site: SiteOutput): Promise<void> {
  const output = join(site.distDir, page.path);
  await mkdir(dirname(output), { recursive: true });
  await writeFile(output, renderPage(page, site.index, site.tasks));
}

function renderSitemap(index: Page, pages: Page[]): string {
  const urls = [{ ...index, path: '' }, ...pages].map((page) => `  <url><loc>${escapeHtml(`${origin}/${page.path}`)}</loc><lastmod>${page.updated}</lastmod><ai:title>${escapeHtml(page.title)}</ai:title><ai:description>${escapeHtml(page.description)}</ai:description></url>`).join('\n');
  return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:ai="${origin}/">\n${urls}\n</urlset>\n`;
}

function renderHeaders(): string {
  return '/*.md\n  Content-Type: text/markdown; charset=utf-8\n  Cache-Control: public, max-age=300\n  X-Robots-Tag: all\n\n/llms.txt\n  Content-Type: text/plain; charset=utf-8\n  Cache-Control: public, max-age=300\n  X-Robots-Tag: all\n';
}

function renderIndex(index: Page, tasks: Page[]): string {
  const links = [{ path: 'llms.txt', title: 'llms.txt' }, ...tasks].map((page) => `<li><a href="/${escapeHtml(page.path)}">${escapeHtml(page.title)}</a></li>`).join('');
  return `<!doctype html>\n<html lang="en"><head><meta charset="utf-8"><meta name="description" content="${escapeHtml(index.description)}"><title>${escapeHtml(index.title)}</title></head><body><h1>${escapeHtml(index.title)}</h1><ul>${links}</ul></body></html>\n`;
}

async function writeDiscovery(site: SiteOutput, pages: Page[]): Promise<void> {
  const { distDir, index, tasks } = site;
  await writeFile(join(distDir, 'sitemap.xml'), renderSitemap(index, pages));
  await writeFile(join(distDir, 'robots.txt'), `User-agent: *\nAllow: /\nSitemap: ${origin}/sitemap.xml\n`);
  await writeFile(join(distDir, '_headers'), renderHeaders());
  await writeFile(join(distDir, 'index.html'), renderIndex(index, tasks));
}

export async function build(options: BuildOptions = {}): Promise<void> {
  const srcDir = options.srcDir ?? join(packageDir, 'src');
  const distDir = options.distDir ?? join(packageDir, 'dist');
  const localeDir = options.localeDir ?? resolve(packageDir, '../../locales/en');
  const date = buildDate(options);
  const cache: CatalogCache = new Map();
  const pages: Page[] = [];
  for (const file of (await files(srcDir)).filter((file) => file.endsWith('.md'))) {
    const path = relative(srcDir, file);
    const template = { path, text: (await readFile(file, 'utf8')).replace(/\r\n/g, '\n') };
    const text = await resolvePlaceholders(template, { localeDir, cache });
    pages.push(parsePage({ path, text }, { date }));
  }
  const index = pages.find((page) => page.path === 'llms.txt');
  if (!index) throw new Error('Missing llms.txt.md template');
  const tasks = pages.filter((page) => page !== index);
  const site = { distDir, index, tasks };
  await rm(distDir, { recursive: true, force: true });
  await mkdir(distDir, { recursive: true });
  for (const page of pages) await writePage(page, site);
  await writeDiscovery(site, pages);
}

function validArgs(args: string[]): boolean {
  return args.length === 0 || args.length === 2 && args[0] === '--date';
}

function cliOptions(args: string[]): BuildOptions {
  if (!validArgs(args)) throw new Error('Usage: bun scripts/build.ts [--date YYYY-MM-DD]');
  return { date: args[1] };
}

if (import.meta.main) await build(cliOptions(process.argv.slice(2)));
