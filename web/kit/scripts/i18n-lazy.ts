// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { Tree, literalString, type AstNode } from './i18n-ast.ts';
import { isNodeModule } from './i18n-slice-graph.ts';

const DYNAMIC_IMPORT = /\bimport\s*\(/;

const isAppModule = (id: string): boolean => APP_CODE.test(id) && !isNodeModule(id);

export interface ModuleHost {
  resolve: (source: string, importer: string) => Promise<string | undefined>;
  parse: (code: string) => AstNode;
}
const APP_CODE = /\/web\/(dashboard\/src|kit)\//;

/**
 * Tracks which app modules are lazily imported, so each can carry its own messages. Rolldown transforms an importer
 * before the module it imports, so a target is known by the time its own transform runs; one that was transformed
 * earlier through a static import simply stays part of its importers' slices.
 */
export class LazyUnits {
  private readonly targets = new Set<string>();
  readonly registered = new Set<string>();

  async note(code: string, importer: string, host: ModuleHost): Promise<void> {
    const imports = APP_CODE.test(importer) && DYNAMIC_IMPORT.test(code);
    if (!imports) return;
    const sources = new Tree(host.parse(code)).nodes.filter((node) => node.type === 'ImportExpression').map((node) => literalString(node.source));
    for (const source of sources.filter((value): value is string => value !== undefined)) {
      const id = (await host.resolve(source, importer)) ?? '';
      if (isAppModule(id)) this.targets.add(id);
    }
  }

  claim(id: string): boolean {
    const lazy = this.targets.has(id);
    if (lazy) this.registered.add(id);
    return lazy;
  }
}
