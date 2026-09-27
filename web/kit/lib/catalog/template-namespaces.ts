// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { lex, parseCond } from '../engine/tmpl';
import type { ModuleDef, ModuleReply } from './module-def';

/** Convert only this reply's published fields. Unknown/dynamic tokens and
 * literal text stay intact, including fallbacks and conditional branch text. */
export function namespaceReplyTemplate(moduleId: string, reply: Pick<ModuleReply, 'tokens'>, template: string): string {
  const prefix = `${moduleId}:`;
  const fields = new Set((reply.tokens ?? []).map((token) => token.name.startsWith(prefix) ? token.name.slice(prefix.length) : token.name));
  return lex(template).map((token) => {
    if (token.kind === 'literal') return token.text;
    if (token.payload === null && fields.has(token.name)) {
      return `{${prefix}${token.name}${token.fallback === null ? '' : `|${token.fallback}`}}`;
    }
    const cond = parseCond(token);
    if (cond && cond.ref.payload === null && fields.has(cond.ref.name)) {
      // A nested opening brace can hide later branches from the shared lexer.
      // Keep these ambiguous conditionals intact during migration.
      if (token.payload!.split(':').length === 2 && token.raw.slice(1).includes('{')) return token.raw;
      // Keep the original comparison and branches byte for byte; only the
      // referenced field changes, so a migration never rewrites user text.
      const migrated = token.raw.replace(/^\{if:[^:=|}]+/i, `{if:${prefix}${cond.ref.name}`);
      if (token.payload!.split(':').length === 2 && !token.raw.slice(1).includes('{')) {
        // Adding the namespace spends a colon. Preserve the original
        // single-branch grammar with an explicit empty else branch.
        const before = token.fallback === null ? migrated.length - 1 : migrated.lastIndexOf('|');
        return `${migrated.slice(0, before)}:${migrated.slice(before)}`;
      }
      return migrated;
    }
    return token.raw;
  }).join('');
}

/** Keep per-module source declarations readable while every public catalog
 * consumer receives canonical namespaced defaults and insert palettes. */
export function namespacedModuleDef(def: ModuleDef): ModuleDef {
  return { ...def, replies: def.replies.map((reply) => ({
    ...reply,
    defaultMessage: namespaceReplyTemplate(def.id, reply, reply.defaultMessage),
    tokens: reply.tokens?.map((token) => ({ ...token, name: token.name.startsWith(`${def.id}:`) ? token.name : `${def.id}:${token.name}` }))
  })) };
}

/** Samples use the same namespace as the reply's public insert palette. */
export function namespaceReplySamples(moduleId: string, samples: Readonly<Record<string, string>>): Record<string, string> {
  return Object.fromEntries(Object.entries(samples).map(([field, sample]) => [field.startsWith(`${moduleId}:`) ? field : `${moduleId}:${field}`, sample]));
}
