// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createHash } from 'node:crypto';
import type { Plugin } from 'vite';
import type { MessageTree } from '../lib/i18n/types.ts';
import { LOCALES_DIR, consoleLocales, readConsoleTree } from '../lib/i18n/tree-fs.ts';

const VIRTUAL = 'virtual:bagel-i18n';
const DATA = `${VIRTUAL}/data/`;
const URL_DIR = '_app/immutable/i18n';

interface Source {
  locales: () => string[];
  read: (locale: string) => MessageTree;
}

interface Options {
  exclude: ReadonlySet<string>;
  source: Source;
}

const UNSAFE_IN_CODE = /[<>/\u{2028}\u{2029}]/gu;
const escapeCode = (json: string) => json.replace(UNSAFE_IN_CODE, (char) => `\\u${char.charCodeAt(0).toString(16).padStart(4, '0')}`);
const toCode = (value: unknown) => escapeCode(JSON.stringify(value));

interface Catalogs {
  locales: string[];
  trees: Record<string, MessageTree>;
  names: Record<string, string>;
  data: Record<string, string>;
  files: Record<string, string>;
  urls: Record<string, string>;
}

const isBranch = (node: unknown): node is MessageTree => typeof node === 'object' && node !== null && !Array.isArray(node);

function overlay(base: MessageTree, top: MessageTree): MessageTree {
  const out: MessageTree = { ...base };
  for (const [key, value] of Object.entries(top)) {
    const existing = out[key];
    out[key] = isBranch(value) && isBranch(existing) ? overlay(existing, value) : value;
  }
  return out;
}

function leafPaths(tree: MessageTree, prefix = ''): string[] {
  return Object.entries(tree).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return isBranch(value) ? leafPaths(value, path) : [path];
  });
}

function assertEnglishKeys(locale: string, english: MessageTree, merged: MessageTree): void {
  const present = new Set(leafPaths(merged));
  const missing = leafPaths(english).filter((path) => !present.has(path));
  if (missing.length) throw new Error(`i18n: ${locale} catalog lacks ${missing.length} English key(s), first ${missing[0]}`);
}

function langName(tree: MessageTree, locale: string): string {
  const name = isBranch(tree.lang) ? tree.lang.name : undefined;
  return typeof name === 'string' ? name : locale;
}

function without(tree: MessageTree, exclude: ReadonlySet<string>): MessageTree {
  return Object.fromEntries(Object.entries(tree).filter(([namespace]) => !exclude.has(namespace)));
}

function assemble({ exclude, source }: Options): Catalogs {
  const locales = source.locales();
  const english = without(source.read('en'), exclude);
  const out: Catalogs = { locales, trees: {}, names: {}, data: {}, files: {}, urls: {} };
  for (const locale of locales) {
    const merged = locale === 'en' ? english : overlay(english, without(source.read(locale), exclude));
    assertEnglishKeys(locale, english, merged);
    const body = JSON.stringify(merged);
    const hash = createHash('sha256').update(body).digest('hex').slice(0, 10);
    const fileName = `${URL_DIR}/${locale}.${hash}.json`;
    out.trees[locale] = merged;
    out.names[locale] = langName(merged, locale);
    out.data[locale] = escapeCode(body);
    out.files[fileName] = body;
    out.urls[locale] = `/${fileName}`;
  }
  return out;
}

interface PluginOptions {
  exclude?: string[];
  source?: Source;
  slices?: boolean;
}

export const CATALOGS_PLUGIN = 'bagel-i18n-catalogs';

export interface CatalogsApi {
  catalogs: () => Pick<Catalogs, 'locales' | 'trees'>;
}

/**
 * Serves the console catalog as one merged tree per locale. The server imports each locale whole. Without slices the
 * browser fetches one content-hashed asset per locale that the server can also name; with slices a production build
 * leaves browser delivery to i18nSlices.
 */
export function i18nCatalogs({ exclude = [], source = { locales: consoleLocales, read: readConsoleTree }, slices = false }: PluginOptions = {}): Plugin<CatalogsApi> {
  const options: Options = { exclude: new Set(exclude), source };
  let cached: Catalogs | undefined;
  let sliced = false;
  const catalogs = () => (cached ??= assemble(options));

  return {
    name: CATALOGS_PLUGIN,
    api: { catalogs },
    configResolved(config) {
      sliced = slices && config.command === 'build';
    },
    resolveId(id) {
      if (id === VIRTUAL || id.startsWith(`${VIRTUAL}/`)) return `\0${id}`;
      return undefined;
    },
    load(id, loadOptions) {
      if (!id.startsWith(`\0${VIRTUAL}`)) return undefined;
      const { locales, names, data, urls } = catalogs();
      const name = id.slice(1);
      if (name.startsWith(DATA)) return `export default ${data[name.slice(DATA.length)]};`;
      const loaders = loadOptions?.ssr
        ? locales.map((locale) => `${toCode(locale)}: () => import(${toCode(DATA + locale)})`).join(',')
        : '';
      return [
        `export const locales = ${toCode(locales)};`,
        `export const localeNames = ${toCode(names)};`,
        `export const urls = ${toCode(sliced ? {} : urls)};`,
        `export const sliced = ${sliced};`,
        `export const loaders = {${loaders}};`
      ].join('\n');
    },
    generateBundle() {
      if (this.environment?.name !== 'client' || sliced) return;
      for (const [fileName, source] of Object.entries(catalogs().files)) this.emitFile({ type: 'asset', fileName, source });
    },
    configureServer(server) {
      server.watcher.add(LOCALES_DIR);
      server.watcher.on('change', (file) => {
        if (!file.startsWith(LOCALES_DIR)) return;
        cached = undefined;
        for (const module of server.moduleGraph.idToModuleMap.values()) {
          if (module.id?.startsWith(`\0${VIRTUAL}`)) server.moduleGraph.invalidateModule(module);
        }
        server.ws.send({ type: 'full-reload' });
      });
      server.middlewares.use(`/${URL_DIR}/`, (req, res, next) => {
        const body = catalogs().files[`${URL_DIR}/${(req.url ?? '').split('?')[0].slice(1)}`];
        if (body === undefined) return next();
        res.setHeader('Content-Type', 'application/json');
        res.end(body);
      });
    }
  };
}
