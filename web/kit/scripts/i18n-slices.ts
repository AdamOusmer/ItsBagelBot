// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createHash } from 'node:crypto';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { Plugin, Rollup } from 'vite';
import { CATALOGS_PLUGIN, type CatalogsApi } from './i18n-catalogs.ts';
import type { AstNode } from './i18n-ast.ts';
import { KeyCatalog, Linker, type LinkHost } from './i18n-link.ts';
import { LazyUnits, type ModuleHost } from './i18n-lazy.ts';
import { assertCoverage, type UnitSlice } from './i18n-slice-check.ts';
import { alwaysLoadedWith, isNodeModule, leafPaths, parseDictionary, pick, type Chunk, type Route } from './i18n-slice-graph.ts';
import { SliceBuilder } from './i18n-slice-units.ts';
import { analyzeModule, type Usage } from './i18n-usage.ts';

export const NODE_PLACEHOLDER = '__BAGEL_I18N_NODE__';
export const SLICE_DIR = '_app/immutable/i18n';
const KIT_DIR = fileURLToPath(new URL('..', import.meta.url));
const APP_MODULE = /\/generated\/(?:build\/)?client-optimized\/app\.js$/;
const EXEMPT = /\/kit\/lib\/i18n\/(messages|context)\.ts$/;
const API_MENTION = /\b(getI18n|setI18n|translate|translateList)\b/;
const PLACEHOLDER_LITERAL = new RegExp(`(["'\`])${NODE_PLACEHOLDER}\\1`, 'g');
const REGISTER = `\nimport { registerMessages as __bagelRegisterMessages } from '@bagel/kit/i18n';\nawait __bagelRegisterMessages(import.meta.url, '${NODE_PLACEHOLDER}');\n`;

const SKIPPED: ((id: string, code: string) => boolean)[] = [
  (id) => id.startsWith('\0'),
  (id) => id.includes('/node_modules/'),
  (id) => EXEMPT.test(id),
  (_id, code) => !API_MENTION.test(code)
];

const hash = (parts: string[]): string => createHash('sha256').update(parts.join('\n')).digest('hex').slice(0, 10);

const sliceFile = (fileName: string): string => fileName.slice(fileName.lastIndexOf('/') + 1).replace(/\.js$/, '.json');

/** Lists the chunks that have slices, for the server to preload; an empty slice is neither emitted nor fetched. */
export const SLICED_CHUNKS = '_app/i18n-slices.json';

const fetched = (slice: UnitSlice): boolean => slice.bodies.en !== '{}';

type Context = Rollup.PluginContext;

function moduleHost(context: Rollup.TransformPluginContext): ModuleHost {
  return {
    resolve: async (source, importer) => (await context.resolve(source, importer))?.id,
    parse: (code) => context.parse(code) as unknown as AstNode
  };
}

function linkHost(context: Context): LinkHost {
  return {
    resolve: async (source, importer) => (await context.resolve(source, importer))?.id,
    code: (id) => context.getModuleInfo(id)?.code ?? undefined,
    exports: (id) => context.getModuleInfo(id)?.exports ?? [],
    parse: (code) => context.parse(code) as unknown as AstNode
  };
}

function clientRoutes(context: Context): Route[] {
  const app = [...context.getModuleIds()].find((id) => APP_MODULE.test(id));
  const code = app && context.getModuleInfo(app)?.code;
  if (!code) context.error('i18n slices: client app module not found');
  return parseDictionary(context.parse(code!) as unknown as AstNode);
}

/** Per-module message keys, resolved across modules; the build stops on any site it cannot bound. */
class KeyIndex {
  readonly moduleKeys = new Map<string, Set<string>>();
  private readonly usages = new Map<string, Usage>();

  record(id: string, usage: Usage): void {
    this.usages.set(id, usage);
  }

  async resolve(context: Context, english: readonly string[]): Promise<void> {
    const linker = new Linker(linkHost(context));
    const catalog = new KeyCatalog(english);
    const problems = new Set<string>();
    for (const [id, usage] of this.usages) {
      const linked = await linker.link(id, usage);
      const matched = catalog.match(linked.patterns);
      this.moduleKeys.set(id, matched.keys);
      [...linked.problems, ...matched.problems].forEach((problem) => problems.add(`${id}: ${problem}`));
    }
    if (problems.size) context.error(`i18n: ${problems.size} translation site(s) are not provably covered:\n${[...problems].join('\n')}`);
  }
}

/**
 * Splits the browser catalog per SvelteKit node. Every translation call in client code must name its key with a
 * literal, a template or concatenation with literal text, a frozen key table, or a translator handed to a function
 * whose own calls follow the same rules; anything else fails the build. Each node chunk carries its English strings
 * and fetches one merged slice per other locale before it evaluates.
 */
export function i18nSlices(): Plugin {
  let catalogs: CatalogsApi['catalogs'] = () => {
    throw new Error(`i18n slices: ${CATALOGS_PLUGIN} plugin missing`);
  };
  const index = new KeyIndex();
  const lazy = new LazyUnits();
  const nodes = new Set<string>();
  const slices = new Map<string, UnitSlice>();
  let ancestors = new Map<number, Set<number>>();
  let routes: Route[] = [];
  let appRoots: string[] = [];

  function builder(chunks: Record<string, Chunk>): SliceBuilder {
    return new SliceBuilder({ chunks, moduleKeys: index.moduleKeys, ancestors, units: new Set([...nodes, ...lazy.registered]) });
  }

  function slice(unit: string, units: SliceBuilder): UnitSlice {
    const { locales, trees } = catalogs();
    const keys = units.ownKeys(unit);
    const bodies = Object.fromEntries(locales.map((locale) => [locale, JSON.stringify(pick(trees[locale], keys))]));
    return { demand: units.demand(unit), bodies, contexts: units.contexts(unit) };
  }

  function emitSlices(context: Context, bundle: Rollup.OutputBundle): void {
    const others = catalogs().locales.filter((locale) => locale !== 'en');
    const chunks = Object.values(bundle).filter((output): output is Rollup.OutputChunk => output.type === 'chunk');
    const sliced: string[] = [];
    for (const chunk of chunks) {
      const unit = chunk.moduleIds.find((id) => slices.has(id));
      const bodies = unit && fetched(slices.get(unit)!) ? slices.get(unit)!.bodies : undefined;
      if (!bodies) continue;
      sliced.push(sliceFile(chunk.fileName).replace(/\.json$/, ''));
      others.forEach((locale) => context.emitFile({ type: 'asset', fileName: `${SLICE_DIR}/${locale}/${sliceFile(chunk.fileName)}`, source: bodies[locale] }));
    }
    context.emitFile({ type: 'asset', fileName: SLICED_CHUNKS, source: JSON.stringify(sliced.sort()) });
  }

  return {
    name: 'bagel-i18n-slices',
    apply: 'build',
    enforce: 'post',
    applyToEnvironment: (environment) => environment.name === 'client',
    configResolved(config) {
      const host = config.plugins.find((plugin) => plugin.name === CATALOGS_PLUGIN) as Plugin<CatalogsApi> | undefined;
      if (host?.api) catalogs = host.api.catalogs;
      appRoots = [join(config.root, 'src'), KIT_DIR];
    },
    async buildStart() {
      await lazy.scan(appRoots, async (source, importer) => (await this.resolve(source, importer))?.id);
    },
    async transform(code, id) {
      await lazy.note(code, id, moduleHost(this));
      if (!SKIPPED.some((skip) => skip(id, code))) index.record(id, analyzeModule(this.parse(code) as unknown as AstNode, code));
      if (isNodeModule(id)) nodes.add(id);
      return nodes.has(id) || lazy.claim(id) ? code + REGISTER : undefined;
    },
    async buildEnd(error) {
      if (error) return;
      const mismatches = lazy.mismatches();
      if (mismatches.length) this.error(`i18n slices: lazy import scan disagrees with the parsed imports on:\n${mismatches.join('\n')}`);
      await index.resolve(this, leafPaths(catalogs().trees.en));
      routes = clientRoutes(this);
      ancestors = alwaysLoadedWith(routes);
    },
    renderChunk(code, chunk, _options, meta) {
      const units = builder(meta.chunks as unknown as Record<string, Chunk>);
      const held = units.unitsIn(chunk);
      if (!held.length) return null;
      const placeholders = [...code.matchAll(PLACEHOLDER_LITERAL)].length;
      if (held.length !== 1 || placeholders !== 1) this.error(`i18n slices: ${chunk.fileName} must hold exactly one message unit, holds ${held.join(', ')}`);
      const unitSlice = slice(held[0], units);
      slices.set(held[0], unitSlice);
      const digest = fetched(unitSlice) ? hash(Object.values(unitSlice.bodies)) : '';
      const payload = `{h:${JSON.stringify(digest)},m:${unitSlice.bodies.en}}`;
      return { code: code.replace(PLACEHOLDER_LITERAL, () => payload), map: null };
    },
    generateBundle(_options, bundle) {
      emitSlices(this, bundle);
      const { locales, trees } = catalogs();
      assertCoverage({ slices, locales, ancestors }, routes, trees.en);
    }
  };
}
