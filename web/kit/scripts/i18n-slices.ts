// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createHash } from 'node:crypto';
import type { Plugin, Rollup } from 'vite';
import { CATALOGS_PLUGIN, type CatalogsApi } from './i18n-catalogs.ts';
import type { AstNode } from './i18n-ast.ts';
import { KeyCatalog, Linker, type LinkHost } from './i18n-link.ts';
import { assertCoverage, type NodeSlice } from './i18n-slice-check.ts';
import { SliceBuilder, alwaysLoadedWith, isNodeModule, leafPaths, nodeIndex, parseDictionary, pick, type Chunk, type Route } from './i18n-slice-graph.ts';
import { analyzeModule, type Usage } from './i18n-usage.ts';

export { alwaysLoadedWith } from './i18n-slice-graph.ts';

export const NODE_PLACEHOLDER = '__BAGEL_I18N_NODE__';
export const SLICE_DIR = '_app/immutable/i18n';
const APP_MODULE = /\/generated\/client-optimized\/app\.js$/;
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

type Context = Rollup.PluginContext;

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
  const slices = new Map<number, NodeSlice>();
  let ancestors = new Map<number, Set<number>>();
  let routes: Route[] = [];

  function slice(node: number, chunks: Record<string, Chunk>): NodeSlice {
    const { locales, trees } = catalogs();
    const builder = new SliceBuilder(chunks, index.moduleKeys, ancestors);
    const keys = builder.ownKeys(node);
    const bodies = Object.fromEntries(locales.map((locale) => [locale, JSON.stringify(pick(trees[locale], keys))]));
    return { demand: builder.demand(node), bodies };
  }

  function emitSlices(context: Context, bundle: Rollup.OutputBundle): void {
    const { locales } = catalogs();
    const nodeChunks = Object.values(bundle).filter((output): output is Rollup.OutputChunk => output.type === 'chunk' && nodeIndex(output.facadeModuleId) !== undefined);
    for (const chunk of nodeChunks) {
      const bodies = slices.get(nodeIndex(chunk.facadeModuleId)!)?.bodies;
      if (!bodies) context.error(`i18n slices: ${chunk.fileName} was not sliced`);
      const others = locales.filter((locale) => locale !== 'en');
      others.forEach((locale) => context.emitFile({ type: 'asset', fileName: `${SLICE_DIR}/${locale}/${sliceFile(chunk.fileName)}`, source: bodies![locale] }));
    }
  }

  return {
    name: 'bagel-i18n-slices',
    apply: 'build',
    enforce: 'post',
    applyToEnvironment: (environment) => environment.name === 'client',
    configResolved(config) {
      const host = config.plugins.find((plugin) => plugin.name === CATALOGS_PLUGIN) as Plugin<CatalogsApi> | undefined;
      if (host?.api) catalogs = host.api.catalogs;
    },
    transform(code, id) {
      if (isNodeModule(id)) return code + REGISTER;
      if (!SKIPPED.some((skip) => skip(id, code))) index.record(id, analyzeModule(this.parse(code) as unknown as AstNode, code));
      return undefined;
    },
    async buildEnd(error) {
      if (error) return;
      await index.resolve(this, leafPaths(catalogs().trees.en));
      routes = clientRoutes(this);
      ancestors = alwaysLoadedWith(routes);
    },
    renderChunk(code, chunk, _options, meta) {
      const node = nodeIndex(chunk.facadeModuleId);
      if (node === undefined) return null;
      if ([...code.matchAll(PLACEHOLDER_LITERAL)].length !== 1) this.error(`i18n slices: node ${node} chunk must hold exactly one message placeholder`);
      const nodeSlice = slice(node, meta.chunks as unknown as Record<string, Chunk>);
      slices.set(node, nodeSlice);
      const payload = `{h:${JSON.stringify(hash(Object.values(nodeSlice.bodies)))},m:${nodeSlice.bodies.en}}`;
      return { code: code.replace(PLACEHOLDER_LITERAL, () => payload), map: null };
    },
    generateBundle(_options, bundle) {
      emitSlices(this, bundle);
      const { locales, trees } = catalogs();
      assertCoverage({ slices, locales }, routes, trees.en);
    }
  };
}
