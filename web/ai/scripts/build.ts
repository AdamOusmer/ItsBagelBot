import { cp, mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { dirname, join, relative, resolve } from 'node:path';

const packageDir = resolve(import.meta.dir, '..');
const origin = 'https://ai.itsbagelbot.com';
const humanOrigin = 'https://itsbagelbot.com/';
const publicDir = join(packageDir, 'public');
const kindHeadings: Record<string, string> = { overview: 'Overview', feature: 'Features', setup: 'Setup', errors: 'Errors', migration: 'Migration' };

type BuildOptions = {
  srcDir?: string;
  distDir?: string;
  localeDir?: string;
};

type Template = { path: string; text: string };
type Page = { path: string; title: string; description: string; updated: string; kind?: string; human?: string; body: string };
type CatalogCache = Map<string, unknown>;
type PlaceholderOptions = { localeDir: string; cache: CatalogCache };
type CatalogReference = { surface: string; keys: string[] };
const fullOrder = ['overview', 'migration', 'feature', 'setup', 'errors'];
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

function dateString(value: string, path: string): string {
  const date = new Date(`${value}T00:00:00Z`);
  const valid = /^\d{4}-\d{2}-\d{2}$/.test(value) && Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value;
  if (!valid) throw new Error(`${path}: invalid front matter updated ${value}`);
  return value;
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
  const fields = lines.map((line) => /^(title|description|updated|kind|human):\s*(.*)$/.exec(line));
  for (const field of fields.filter((field): field is RegExpExecArray => field !== null)) {
    metadata[field[1]] = field[2].replace(/^(['"])(.*)\1$/, '$2');
  }
  return metadata;
}

function checkOptionalFields(metadata: Record<string, string>, path: string): void {
  if (metadata.kind !== undefined && !Object.hasOwn(kindHeadings, metadata.kind)) throw new Error(`${path}: invalid front matter kind ${metadata.kind}`);
  if (metadata.human !== undefined && !metadata.human.startsWith(humanOrigin)) throw new Error(`${path}: front matter human must start with ${humanOrigin}`);
}

function parsePage(template: Template): Page {
  const frontMatter = /^---\n([\s\S]*?)\n---(?:\n|$)/.exec(template.text);
  if (!frontMatter) throw new Error(`${template.path}: missing front matter`);
  const metadata = parseFrontMatter(frontMatter[1].split('\n'));
  const missing = ['title', 'description', 'updated'].find((field) => !metadata[field]);
  if (missing) throw new Error(`${template.path}: missing front matter ${missing}`);
  checkOptionalFields(metadata, template.path);
  return {
    path: template.path === 'llms.txt.md' ? 'llms.txt' : template.path,
    title: metadata.title,
    description: metadata.description,
    updated: dateString(metadata.updated, template.path),
    kind: metadata.kind,
    human: metadata.human,
    body: template.text.slice(frontMatter[0].length).trim(),
  };
}

function taskLine(task: Page): string {
  return `- [${task.title}](${origin}/${task.path}): ${task.description}`;
}

function renderTasks(tasks: Page[]): string {
  const sorted = [...tasks].sort((a, b) => a.path.localeCompare(b.path, 'en'));
  const groups = [...Object.entries(kindHeadings).map(([kind, heading]) => ({ heading, kind })), { heading: 'Tasks', kind: undefined }];
  return groups
    .map(({ heading, kind }) => ({ heading, items: sorted.filter((task) => task.kind === kind) }))
    .filter((group) => group.items.length > 0)
    .map((group) => `## ${group.heading}\n\n${group.items.map(taskLine).join('\n')}\n`)
    .join('\n');
}

function renderPage(page: Page, index: Page, tasks: Page[]): string {
  const markdown = `# ${page.title}\n\n${page.body}`.trimEnd() + '\n';
  if (page !== index) return markdown;
  return markdown + '\n' + renderTasks(tasks);
}

async function writePage(page: Page, site: SiteOutput): Promise<void> {
  const output = join(site.distDir, page.path);
  await mkdir(dirname(output), { recursive: true });
  await writeFile(output, renderPage(page, site.index, site.tasks));
}

function fullRank(page: Page): number {
  const rank = fullOrder.indexOf(page.kind ?? '');
  return rank === -1 ? fullOrder.length : rank;
}

function renderFull(tasks: Page[]): string {
  const sorted = [...tasks].sort((a, b) => fullRank(a) - fullRank(b) || a.path.localeCompare(b.path, 'en'));
  const intro = '# ItsBagelBot full reference for AI assistants\n\nThe per-page files listed in llms.txt are canonical; this file concatenates them for single-fetch use.\n';
  const sections = sorted.map((page) => `Source: ${origin}/${page.path}\nUpdated: ${page.updated}\n\n${`# ${page.title}\n\n${page.body}`.trimEnd()}\n`);
  return [intro, ...sections].join('\n---\n\n');
}

function renderSitemap(pages: Page[]): string {
  const urls = pages.filter((page) => page.path !== 'llms.txt').map((page) => `  <url><loc>${escapeHtml(`${origin}/${page.path}`)}</loc><lastmod>${page.updated}</lastmod></url>`).join('\n');
  return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${urls}\n</urlset>\n`;
}

function headerBlock(path: string, headers: string[]): string {
  return `${path}\n${headers.map((header) => `  ${header}`).join('\n')}\n`;
}

function renderHeaders(tasks: Page[]): string {
  const markdown = headerBlock('/*.md', ['Content-Type: text/markdown; charset=utf-8', 'Cache-Control: public, max-age=300', 'X-Robots-Tag: all']);
  const canonicals = tasks.map((task) => headerBlock(`/${task.path}`, [`Link: <${origin}/${task.path}>; rel="canonical"`]));
  const noindex = ['X-Robots-Tag: noindex, follow'];
  const text = headerBlock('/llms.txt', ['Content-Type: text/plain; charset=utf-8', 'Cache-Control: public, max-age=300', ...noindex]);
  const full = headerBlock('/llms-full.txt', ['Content-Type: text/plain; charset=utf-8', 'Cache-Control: public, max-age=300', ...noindex]);
  return [markdown, ...canonicals, text, full, headerBlock('/', noindex)].join('\n');
}

function renderLinks(tasks: Page[]): string {
  const links = [{ path: 'llms.txt', title: 'llms.txt' }, ...tasks].map((page) => `<li><a href="/${escapeHtml(page.path)}">${escapeHtml(page.title)}</a></li>`);
  return `<ul>${links.join('')}</ul>`;
}

function renderIndex(index: Page, tasks: Page[]): string {
  const head = `<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="robots" content="noindex, follow"><link rel="canonical" href="${origin}/"><meta name="description" content="${escapeHtml(index.description)}"><title>${escapeHtml(index.title)}</title>`;
  const human = `<p>This site is written for AI assistants. For the human site, go to <a href="${humanOrigin}">${humanOrigin}</a>.</p>`;
  return `<!doctype html>\n<html lang="en"><head>${head}</head><body><h1>${escapeHtml(index.title)}</h1>${human}${renderLinks(tasks)}</body></html>\n`;
}

function renderNotFound(): string {
  const head = '<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="robots" content="noindex, follow"><title>Page not found</title>';
  return `<!doctype html>\n<html lang="en"><head>${head}</head><body><h1>Page not found</h1><p>Start from the <a href="/">index of pages</a> or visit <a href="${humanOrigin}">${humanOrigin}</a>.</p></body></html>\n`;
}

async function writeDiscovery(site: SiteOutput, pages: Page[]): Promise<void> {
  const { distDir, index, tasks } = site;
  await writeFile(join(distDir, 'sitemap.xml'), renderSitemap(pages));
  await writeFile(join(distDir, 'robots.txt'), `User-agent: *\nAllow: /\nSitemap: ${origin}/sitemap.xml\n`);
  await writeFile(join(distDir, 'llms-full.txt'), renderFull(tasks));
  await writeFile(join(distDir, '_headers'), renderHeaders(tasks));
  await writeFile(join(distDir, 'index.html'), renderIndex(index, tasks));
  await writeFile(join(distDir, '404.html'), renderNotFound());
}

export async function build(options: BuildOptions = {}): Promise<void> {
  const srcDir = options.srcDir ?? join(packageDir, 'src');
  const distDir = options.distDir ?? join(packageDir, 'dist');
  const localeDir = options.localeDir ?? resolve(packageDir, '../../locales/en');
  const cache: CatalogCache = new Map();
  const pages: Page[] = [];
  for (const file of (await files(srcDir)).filter((file) => file.endsWith('.md'))) {
    const path = relative(srcDir, file);
    const template = { path, text: (await readFile(file, 'utf8')).replace(/\r\n/g, '\n') };
    const text = await resolvePlaceholders(template, { localeDir, cache });
    pages.push(parsePage({ path, text }));
  }
  const index = pages.find((page) => page.path === 'llms.txt');
  if (!index) throw new Error('Missing llms.txt.md template');
  const tasks = pages.filter((page) => page !== index);
  const site = { distDir, index, tasks };
  await rm(distDir, { recursive: true, force: true });
  await mkdir(distDir, { recursive: true });
  for (const page of pages) await writePage(page, site);
  await writeDiscovery(site, pages);
  await cp(publicDir, distDir, { recursive: true }).catch(missingCatalog);
}

if (import.meta.main) await build();
