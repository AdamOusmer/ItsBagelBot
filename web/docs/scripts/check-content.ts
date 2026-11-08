// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { readdirSync, readFileSync, existsSync, statSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { resolvedSpaces } from '../src/lib/spaces';
import { parseDocument, pageLinkError } from './content-policy';

const root = resolve(import.meta.dir, '..');
const repository = resolve(root, '../..');
const content = resolve(root, 'src/content/docs');
const files = readdirSync(content, { recursive: true, encoding: 'utf8' }).filter((file) => file.endsWith('.md'));
const idOf = (file: string) => file.replace(/\.md$/, '').replace(/\/index$/, '');
const ids = new Set(files.map(idOf));
const fileById = new Map(files.map((file) => [idOf(file), file]));
const english = files.filter((file) => !file.startsWith('fr/'));
const spaces = resolvedSpaces(english.map(idOf));
const listed = spaces.flatMap((space) => space.groups.flatMap((group) => group.pages));
const pages = new Map(files.map((file) => [idOf(file), parseDocument(readFileSync(resolve(content, file), 'utf8'))]));
const errors: string[] = [];
const diagrams: { file: string; source: string }[] = [];
for (const file of english) {
  const id = idOf(file);
  if (listed.filter((page) => page === id).length !== 1) errors.push(`${file}: must occur exactly once in navigation`);
  const translated = pages.get(`fr/${id}`);
  if (!translated) errors.push(`${file}: French counterpart missing`);
  else {
    if (translated.docType !== pages.get(id)!.docType) errors.push(`${file}: translation docType differs`);
    if (translated.diagrams.length !== pages.get(id)!.diagrams.length) errors.push(`${file}: translation diagram count differs`);
  }
}
for (const id of listed) if (!ids.has(id)) errors.push(`Navigation target missing: ${id}`);
for (const space of spaces) for (const id of space.featured) {
  if (!space.groups.some((group) => group.pages.includes(id))) errors.push(`Featured page ${id} does not belong to ${space.key}`);
}
for (const [id, document] of pages) {
  if (id.startsWith('fr/') && !ids.has(id.slice(3))) errors.push(`${id}: English counterpart missing`);
  errors.push(...document.errors.map((error) => `${id}: ${error}`));
  diagrams.push(...document.diagrams.map((source) => ({ file: id, source })));
  for (const asset of document.assets) {
    if (/^https?:\/\//.test(asset)) continue;
    const assetPath = decodeURIComponent(asset.split(/[?#]/)[0]);
    const target = assetPath.startsWith('/') ? resolve(root, 'public', assetPath.slice(1)) : resolve(dirname(resolve(content, fileById.get(id)!)), assetPath);
    if (!existsSync(target)) errors.push(`${id}: missing image ${asset}`);
  }
  for (const href of document.links) {
    try {
      const error = pageLinkError(href, id, pages);
      if (error) errors.push(`${id}: ${error}`);
      const source = href.match(/^https:\/\/github\.com\/AdamOusmer\/ItsBagelBot\/(blob|tree)\/main\/([^?#]+)/);
      if (source) {
        const target = resolve(repository, decodeURIComponent(source[2]));
        if (!existsSync(target)) errors.push(`${id}: missing source ${source[2]}`);
        else if (statSync(target).isDirectory() !== (source[1] === 'tree')) errors.push(`${id}: source link must use ${statSync(target).isDirectory() ? 'tree' : 'blob'}: ${href}`);
      }
    } catch (error) { errors.push(`${id}: invalid link ${href}: ${String(error)}`); }
  }
}
for (const prefix of ['', 'fr/']) {
  const records = files.filter((file) => new RegExp(`^${prefix}adr/\\d`).test(file)).sort();
  records.forEach((file, index) => {
    if (!file.startsWith(`${prefix}adr/${String(index + 1).padStart(4, '0')}-`)) errors.push(`${file}: ADR sequence is not contiguous`);
  });
}
if (errors.length) { console.error(errors.join('\n')); process.exit(1); }
console.log(`Checked ${files.length} pages: metadata, headings, navigation, bilingual parity, links, anchors, sources and ADR numbering.`);
if (process.argv.includes('--diagrams')) {
  // Use a real DOM: Mermaid's sanitizer and SVG layout need browser APIs.
  const { chromium } = await import('@playwright/test');
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage();
    await page.setContent('<!doctype html><html><body></body></html>');
    await page.addScriptTag({ path: resolve(root, '../node_modules/mermaid/dist/mermaid.min.js') });
    const failures = await page.evaluate(async (items) => {
      const mermaid = (window as any).mermaid;
      mermaid.initialize({ startOnLoad: false, securityLevel: 'strict' });
      const failures: string[] = [];
      for (let i = 0; i < items.length; i++) {
        try {
          const { svg } = await mermaid.render(`check-${i}`, items[i].source);
          if (!svg.includes('<svg')) throw new Error('No SVG output');
        } catch (error) { failures.push(`${items[i].file}: ${String(error)}`); }
      }
      return failures;
    }, diagrams);
    if (failures.length) { console.error(failures.join('\n')); process.exitCode = 1; }
    else console.log(`Rendered ${diagrams.length} Mermaid diagrams in Chromium.`);
  } finally { await browser.close(); }
}
