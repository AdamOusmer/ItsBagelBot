// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readFileSync, realpathSync } from 'node:fs';
import { createRequire } from 'node:module';
import { basename, dirname, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

/** @typedef {{ type: 'chunk', fileName: string, moduleIds: string[], imports: string[], dynamicImports: string[] }} Chunk */

const EMPTY = '\0bagel-bundled-ui-sheet';
const ROUTE_FILE = /\/src\/routes\/(?:.*\/)?\+[^/]+\.svelte$/;
const require = createRequire(import.meta.url);
const STYLES = resolve(dirname(fileURLToPath(import.meta.url)), '../styles');
const ATTRIBUTE_SHEETS = new Set(['motion.css', 'reveal.css']);

/** @param {string} ref @param {string} from */
function resolveImport(ref, from) {
  return ref.startsWith('.') ? resolve(dirname(from), ref) : realpathSync(require.resolve(ref));
}

/** @param {string} path @returns {string[]} */
const importsOf = (path) =>
  [...readFileSync(path, 'utf8').matchAll(/@import\s+'([^']+\.css)'/g)].map((match) => resolveImport(String(match[1]), path));

/** @param {string} path @param {string[]} into */
function sheetsOf(path, into = []) {
  for (const sheet of importsOf(path)) {
    sheetsOf(sheet, into);
    if (!into.includes(sheet)) into.push(sheet);
  }
  return into;
}

/** @param {string} sheet */
const named = (sheet) => relative(STYLES, sheet);

/** @param {number[]} ranks */
const ascending = (ranks) => ranks.slice(1).every((rank, i) => rank > (ranks[i] ?? Infinity));

/** @param {string[]} order @param {string[]} first @param {string[]} second */
function splitProblems(order, first, second) {
  const rank = new Map([...order, ...second.filter((s) => !order.includes(s))].map((s, i) => [s, i]));
  const ranksOf = (/** @type {string[]} */ list) => list.map((s) => rank.get(s) ?? -1);
  const problems = first.filter((s) => second.includes(s)).map((s) => `${named(s)} is in both lists`);
  if (!ascending([...ranksOf(first), ...ranksOf(second)])) {
    problems.push('ui-public.css then ui-console.css must follow @bagel/ui/styles/elements.css order');
  }
  return problems;
}

/** @param {unknown} output @returns {output is Chunk} */
const isChunk = (output) => /** @type {{ type?: string }} */ (output).type === 'chunk';

/** @param {Record<string, unknown>} bundle @param {(routeFile: string) => boolean} pick */
function modulesOfRoutes(bundle, pick) {
  const chunks = new Map(Object.values(bundle).filter(isChunk).map((c) => [c.fileName, c]));
  const picked = (/** @type {Chunk} */ chunk) => chunk.moduleIds.some((id) => ROUTE_FILE.test(id) && pick(id));
  const seen = new Set([...chunks.values()].filter(picked).map((c) => c.fileName));
  const modules = new Set();
  for (const file of seen) {
    const chunk = chunks.get(file);
    if (!chunk) continue;
    chunk.moduleIds.forEach((id) => modules.add(id));
    [...chunk.imports, ...chunk.dynamicImports].filter((next) => chunks.has(next)).forEach((next) => seen.add(next));
  }
  return modules;
}

/**
 * Element sheets ship in two ordered files: kit ui-public.css on every page, ui-console.css after it on
 * console pages. Adapter imports of those sheets resolve to an empty module. A ui sheet in neither list
 * keeps its own route stylesheet and may only be rendered by public routes.
 * @param {{ consoleRoutes?: RegExp }} [options] route files that import ui-console.css; omit when every page does
 * @returns {import('vite').Plugin}
 */
export function bundledUiSheets({ consoleRoutes } = {}) {
  const order = sheetsOf(realpathSync(require.resolve('@bagel/ui/styles/elements.css')));
  const publicSheets = sheetsOf(resolve(STYLES, 'ui-public.css'));
  const consoleSheets = sheetsOf(resolve(STYLES, 'ui-console.css'));
  const publicListed = importsOf(resolve(STYLES, 'ui-public.css'));
  const problems = splitProblems(order, publicSheets, consoleSheets);
  if (problems.length) throw new Error(`kit ui sheet lists:\n  ${problems.join('\n  ')}`);
  const bundled = new Set([...publicSheets, ...consoleSheets]);
  const unbundled = order.filter((s) => !bundled.has(s));
  const tracked = new Set([...order, ...bundled]);
  const isConsole = (/** @type {string} */ routeFile) => !consoleRoutes || consoleRoutes.test(routeFile);
  /** @type {Map<string, Set<string>>} */
  const sheetsByImporter = new Map();
  let ssr = false;

  /** @param {Set<string>} modules */
  const usedBy = (modules) => new Set([...modules].flatMap((id) => [...(sheetsByImporter.get(id) ?? [])]));

  /** @param {Record<string, unknown>} bundle */
  function bundleProblems(bundle) {
    const onConsole = usedBy(modulesOfRoutes(bundle, isConsole));
    const leaked = unbundled.filter((s) => onConsole.has(s)).map((s) => `add ${named(s)} to ui-console.css: a console route renders it`);
    if (!consoleRoutes) return leaked;
    const onPublic = usedBy(modulesOfRoutes(bundle, (routeFile) => !isConsole(routeFile)));
    const late = consoleSheets.filter((s) => onPublic.has(s)).map((s) => `move ${named(s)} to ui-public.css: a public route renders it`);
    const unrendered = publicListed.filter((s) => !onPublic.has(s) && !ATTRIBUTE_SHEETS.has(basename(s)));
    return [...leaked, ...late, ...unrendered.map((s) => `move ${named(s)} out of ui-public.css: no public route renders it`)];
  }

  /** @param {string} importer @param {string} sheet */
  function track(importer, sheet) {
    const sheets = sheetsByImporter.get(importer) ?? new Set();
    sheets.add(sheet);
    sheetsByImporter.set(importer, sheets);
  }

  return {
    name: 'bagel-bundled-ui-sheets',
    enforce: 'pre',
    configResolved(config) {
      ssr = !!config.build.ssr;
    },
    async resolveId(source, importer) {
      if (!importer || !source.endsWith('.css')) return null;
      if (importer.split('?')[0]?.endsWith('.css')) return null;
      const resolved = await this.resolve(source, importer, { skipSelf: true });
      if (!resolved || !tracked.has(resolved.id)) return null;
      track(importer, resolved.id);
      return bundled.has(resolved.id) ? EMPTY : null;
    },
    load(id) {
      return id === EMPTY ? 'export {};' : null;
    },
    generateBundle(_, bundle) {
      if (!ssr) return;
      const problems = bundleProblems(bundle);
      if (problems.length) this.error(`kit ui sheet lists:\n  ${problems.join('\n  ')}`);
    }
  };
}
