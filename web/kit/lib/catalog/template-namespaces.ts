// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { lex, parseCond, type Token, type VarToken } from '../engine/tmpl';
import type { ModuleDef, ModuleReply } from './module-def';

/** Convert only this reply's published fields. Unknown/dynamic tokens and
 * literal text stay intact, including fallbacks and conditional branch text. */
export function namespaceReplyTemplate(moduleId: string, reply: Pick<ModuleReply, 'tokens'>, template: string): string {
  const prefix = `${moduleId}:`;
  const fields = replyFieldNames(prefix, reply);
  return lex(template).map((token) => namespaceReplyToken(prefix, fields, token)).join('');
}

function replyFieldNames(prefix: string, reply: Pick<ModuleReply, 'tokens'>): Set<string> {
  return new Set((reply.tokens ?? []).map((token) =>
    token.name.startsWith(prefix) ? token.name.slice(prefix.length) : token.name
  ));
}

function isLegacyReplyField(token: VarToken, fields: ReadonlySet<string>): boolean {
  return token.payload === null && fields.has(token.name);
}

function namespaceReplyToken(prefix: string, fields: ReadonlySet<string>, token: Token): string {
  if (token.kind === 'literal') return token.text;
  if (isLegacyReplyField(token, fields)) {
    return `{${prefix}${token.name}${token.fallback === null ? '' : `|${token.fallback}`}}`;
  }
  const cond = parseCond(token);
  if (cond === null || !isLegacyReplyField(cond.ref, fields)) return token.raw;
  return namespaceReplyConditional(prefix, token, cond.ref.name);
}

function namespaceReplyConditional(prefix: string, token: VarToken, field: string): string {
  const singleBranch = token.payload!.split(':').length === 2;
  // A nested opening brace can hide later branches from the shared lexer.
  // Keep these ambiguous conditionals intact during migration.
  if (singleBranch && token.raw.slice(1).includes('{')) return token.raw;
  // Preserve the comparison and branches byte for byte; only the reference changes.
  const migrated = token.raw.replace(/^\{if:[^:=|}]+/i, `{if:${prefix}${field}`);
  return singleBranch ? appendEmptyElse(migrated, token.fallback) : migrated;
}

function appendEmptyElse(template: string, fallback: string | null): string {
  // Adding the namespace spends a colon. Preserve the original single-branch
  // grammar with an explicit empty else branch before any fallback.
  const before = fallback === null ? template.length - 1 : template.lastIndexOf('|');
  return `${template.slice(0, before)}:${template.slice(before)}`;
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
