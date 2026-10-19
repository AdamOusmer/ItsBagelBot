// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createHash } from 'node:crypto';
import type { Plugin } from 'vite';
import type { MessageTree } from '../lib/i18n/types';
import { LOCALES_DIR, consoleLocales, readConsoleTree } from '../lib/i18n/tree-fs';

const VIRTUAL = 'virtual:bagel-i18n';
const DATA = `${VIRTUAL}/data/`;
const URL_DIR = '_app/immutable/i18n';
const CORE = 'core';

type Groups = Record<string, string[]>;

const isBranch = (node: unknown): node is MessageTree => typeof node === 'object' && node !== null && !Array.isArray(node);

function overlay(base: MessageTree, top: MessageTree): MessageTree {
  const out: MessageTree = { ...base };
  for (const [key, value] of Object.entries(top)) {
    const existing = out[key];
    out[key] = isBranch(value) && isBranch(existing) ? overlay(existing, value) : value;
  }
  return out;
}

function partition(tree: MessageTree, exclude: ReadonlySet<string>, groups: Groups): Record<string, MessageTree> {
  const parts: Record<string, MessageTree> = { [CORE]: {} };
  for (const name of Object.keys(groups)) parts[name] = {};
  const owner = new Map<string, string>();
  for (const [group, namespaces] of Object.entries(groups)) {
    for (const namespace of namespaces) {
      if (owner.has(namespace)) throw new Error(`i18n: namespace ${namespace} is in groups ${owner.get(namespace)} and ${group}`);
      owner.set(namespace, group);
    }
  }
  for (const [namespace, value] of Object.entries(tree)) {
    if (exclude.has(namespace)) continue;
    parts[owner.get(namespace) ?? CORE][namespace] = value;
  }
  return parts;
}

interface Options {
  exclude: ReadonlySet<string>;
  groups: Groups;
}

function assemble({ exclude, groups }: Options) {
  const locales = consoleLocales();
  const trees = Object.fromEntries(locales.map((locale) => [locale, readConsoleTree(locale)]));
  if (!trees.en) throw new Error('i18n: missing the en console catalog');

  const data: Record<string, string> = {};
  const files: Record<string, string> = {};
  const urls: Record<string, Record<string, string>> = {};
  const names: Record<string, string> = {};
  for (const locale of locales) {
    const merged = locale === 'en' ? trees.en : overlay(trees.en, trees[locale]);
    const langName = isBranch(merged.lang) ? merged.lang.name : undefined;
    names[locale] = typeof langName === 'string' ? langName : locale;
    data[locale] = JSON.stringify(Object.fromEntries(Object.entries(merged).filter(([namespace]) => !exclude.has(namespace))));
    urls[locale] = {};
    for (const [part, tree] of Object.entries(partition(merged, exclude, groups))) {
      if (part !== CORE && Object.keys(tree).length === 0) continue;
      const body = JSON.stringify(tree);
      const hash = createHash('sha256').update(body).digest('hex').slice(0, 10);
      const fileName = `${URL_DIR}/${locale}.${part}.${hash}.json`;
      files[fileName] = body;
      urls[locale][part] = `/${fileName}`;
    }
  }
  return { locales, names, data, files, urls };
}

interface PluginOptions {
  exclude?: string[];
  groups?: Groups;
}

/**
 * Serves the console catalog as one merged tree per locale. The server imports each locale whole; the browser
 * fetches a core part plus the lazy groups a route names, each a content-hashed asset the server can also name.
 */
export function i18nCatalogs({ exclude = [], groups = {} }: PluginOptions = {}): Plugin {
  const options: Options = { exclude: new Set(exclude), groups };
  let cached: ReturnType<typeof assemble> | undefined;
  const catalogs = () => (cached ??= assemble(options));

  return {
    name: 'bagel-i18n-catalogs',
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
        ? locales.map((locale) => `${JSON.stringify(locale)}: () => import(${JSON.stringify(DATA + locale)})`).join(',')
        : '';
      return [
        `export const locales = ${JSON.stringify(locales)};`,
        `export const localeNames = ${JSON.stringify(names)};`,
        `export const groupNames = ${JSON.stringify(Object.keys(groups))};`,
        `export const urls = ${JSON.stringify(urls)};`,
        `export const loaders = {${loaders}};`
      ].join('\n');
    },
    generateBundle() {
      if (this.environment?.name !== 'client') return;
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
