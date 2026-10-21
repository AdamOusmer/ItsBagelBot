// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { Tree, literalString, type AstNode } from './i18n-ast.ts';
import { isNodeModule } from './i18n-slice-graph.ts';

const DYNAMIC_IMPORT = /\bimport\s*\(/;
const LITERAL_IMPORT = /\bimport\s*\(\s*(['"])([^'"]+)\1\s*\)/g;
const SOURCE_FILE = /\.(svelte|ts|js)$/;
const NOT_CLIENT_FILE = /\.(test|d)\.ts$/;
const NOT_CLIENT_DIR = new Set(['node_modules', 'scripts']);

const isAppModule = (id: string): boolean => APP_CODE.test(id) && !isNodeModule(id);

export interface ModuleHost {
  resolve: (source: string, importer: string) => Promise<string | undefined>;
  parse: (code: string) => AstNode;
}
const APP_CODE = /\/web\/(dashboard\/src|kit)\//;

function sourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return NOT_CLIENT_DIR.has(entry.name) ? [] : sourceFiles(path);
    return SOURCE_FILE.test(entry.name) && !NOT_CLIENT_FILE.test(entry.name) ? [path] : [];
  });
}

/**
 * Tracks which app modules are lazily imported, so each can carry its own messages. Rolldown transforms modules
 * concurrently, so a target that also has static importers can be transformed before its dynamic importer; the
 * targets are therefore read from source before any transform, and the parsed imports only confirm the scan.
 */
export class LazyUnits {
  private readonly targets = new Set<string>();
  private readonly parsed = new Set<string>();
  readonly registered = new Set<string>();

  async scan(roots: readonly string[], resolve: ModuleHost['resolve']): Promise<void> {
    for (const file of roots.flatMap(sourceFiles)) {
      for (const [, , source] of readFileSync(file, 'utf8').matchAll(LITERAL_IMPORT)) {
        const id = (await resolve(source, file)) ?? '';
        if (isAppModule(id)) this.targets.add(id);
      }
    }
  }

  async note(code: string, importer: string, host: ModuleHost): Promise<void> {
    const imports = APP_CODE.test(importer) && DYNAMIC_IMPORT.test(code);
    if (!imports) return;
    const sources = new Tree(host.parse(code)).nodes.filter((node) => node.type === 'ImportExpression').map((node) => literalString(node.source));
    for (const source of sources.filter((value): value is string => value !== undefined)) {
      const id = (await host.resolve(source, importer)) ?? '';
      if (isAppModule(id)) this.parsed.add(id);
    }
  }

  claim(id: string): boolean {
    const lazy = this.targets.has(id);
    if (lazy) this.registered.add(id);
    return lazy;
  }

  /** Modules the source scan and the parsed imports disagree on; any of them makes the claims order dependent. */
  mismatches(): string[] {
    const missed = [...this.parsed].filter((id) => !this.targets.has(id));
    const spurious = [...this.registered].filter((id) => !this.parsed.has(id));
    return [...missed, ...spurious];
  }
}
