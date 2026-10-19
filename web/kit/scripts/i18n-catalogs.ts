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

interface Options {
  exclude: ReadonlySet<string>;
  groups: Groups;
}

interface Catalogs {
  locales: string[];
  names: Record<string, string>;
  data: Record<string, string>;
  files: Record<string, string>;
  urls: Record<string, Record<string, string>>;
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

function groupOwners(groups: Groups): Map<string, string> {
  const owner = new Map<string, string>();
  for (const [group, namespaces] of Object.entries(groups)) {
    for (const namespace of namespaces) {
      if (owner.has(namespace)) throw new Error(`i18n: namespace ${namespace} is in groups ${owner.get(namespace)} and ${group}`);
      owner.set(namespace, group);
    }
  }
  return owner;
}

function partition(tree: MessageTree, { exclude, groups }: Options): Record<string, MessageTree> {
  const owner = groupOwners(groups);
  const parts: Record<string, MessageTree> = { [CORE]: {} };
  for (const name of Object.keys(groups)) parts[name] = {};
  for (const [namespace, value] of Object.entries(tree).filter(([namespace]) => !exclude.has(namespace))) {
    parts[owner.get(namespace) ?? CORE][namespace] = value;
  }
  return parts;
}

function langName(tree: MessageTree, locale: string): string {
  const name = isBranch(tree.lang) ? tree.lang.name : undefined;
  return typeof name === 'string' ? name : locale;
}

function emitParts(locale: string, merged: MessageTree, options: Options, out: Catalogs): void {
  out.urls[locale] = {};
  for (const [part, tree] of Object.entries(partition(merged, options))) {
    if (part !== CORE && Object.keys(tree).length === 0) continue;
    const body = JSON.stringify(tree);
    const hash = createHash('sha256').update(body).digest('hex').slice(0, 10);
    const fileName = `${URL_DIR}/${locale}.${part}.${hash}.json`;
    out.files[fileName] = body;
    out.urls[locale][part] = `/${fileName}`;
  }
}

function assemble(options: Options): Catalogs {
  const locales = consoleLocales();
  const english = readConsoleTree('en');
  const out: Catalogs = { locales, names: {}, data: {}, files: {}, urls: {} };
  for (const locale of locales) {
    const merged = locale === 'en' ? english : overlay(english, readConsoleTree(locale));
    out.names[locale] = langName(merged, locale);
    out.data[locale] = JSON.stringify(Object.fromEntries(Object.entries(merged).filter(([namespace]) => !options.exclude.has(namespace))));
    emitParts(locale, merged, options, out);
  }
  return out;
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
  let cached: Catalogs | undefined;
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
