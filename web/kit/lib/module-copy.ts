// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.


import { namespaceReplyTemplate } from './catalog/template-namespaces';
import type { MessageKey } from './i18n/keys';

import type { Perm } from './types';

import type { ModuleCommandInfo, ModuleDef, ModuleField, ModuleReply } from './catalog/module-def';

type TFn = (key: MessageKey, params?: Record<string, string | number>) => string;

export function catalogKey(id: string, ...parts: string[]): MessageKey {
  return ['modules', 'catalog', id, ...parts].join('.') as MessageKey;
}

export function tCatalog(t: TFn, key: MessageKey, fallback: string): string {
  const value = t(key);
  return value === key ? fallback : value;
}

interface Lookup {
  key: string;
  value: string;
}

function orFallback({ key, value }: Lookup, fallback: string): string {
  return value === key ? fallback : value;
}

export function tModuleLabel(t: TFn, def: Pick<ModuleDef, 'id' | 'label'>): string {
  const key = `modules.catalog.${def.id}.label`;
  return orFallback({ key, value: t(key) }, def.label);
}

export function tModuleTagline(t: TFn, def: Pick<ModuleDef, 'id' | 'tagline'>): string {
  const key = `modules.catalog.${def.id}.tagline`;
  return orFallback({ key, value: t(key) }, def.tagline);
}

export function tModuleDescription(t: TFn, def: Pick<ModuleDef, 'id' | 'description'>): string {
  const key = `modules.catalog.${def.id}.description`;
  return orFallback({ key, value: t(key) }, def.description);
}

export function tModuleFieldPart(
  t: TFn,
  moduleId: string,
  field: ModuleField,
  part: 'label' | 'help' | 'placeholder'
): string {
  const specific = `modules.catalog.${moduleId}.settings.${field.key}.${part}`;
  const shared = `modules.catalog.shared.${field.key}.${part}`;
  return orFallback({ key: specific, value: t(specific) }, orFallback({ key: shared, value: t(shared) }, field[part] ?? ''));
}

export function tModuleFieldOption(
  t: TFn,
  moduleId: string,
  fieldKey: string,
  opt: { value: string; label: string }
): string {
  const key = `modules.catalog.${moduleId}.settings.${fieldKey}.options.${opt.value}`;
  return orFallback({ key, value: t(key) }, opt.label);
}

export function tModuleReplyPart(
  t: TFn,
  moduleId: string,
  reply: ModuleReply,
  part: 'label' | 'tagline' | 'event'
): string {
  const key = `modules.catalog.${moduleId}.replies.${reply.key}.${part}`;
  return orFallback({ key, value: t(key) }, reply[part]);
}

export function tModuleReplyDefault(t: TFn, moduleId: string, reply: ModuleReply): string {
  const key = `modules.catalog.${moduleId}.replies.${reply.key}.defaultMessage`;
  return namespaceReplyTemplate(moduleId, reply, orFallback({ key, value: t(key) }, reply.defaultMessage));
}

export function commandSummarySlug(trigger: string): string {
  return trigger
    .replace(/^!/, '')
    .replace(/[^a-zA-Z0-9]+/g, '_')
    .replace(/^_|_$/g, '');
}

export function tModuleCommandSummary(t: TFn, moduleId: string, command: ModuleCommandInfo): string {
  const key = `modules.catalog.${moduleId}.commands.${commandSummarySlug(command.trigger)}`;
  return orFallback({ key, value: t(key) }, command.summary);
}

export function tModuleCommandPreview(t: TFn, moduleId: string, command: ModuleCommandInfo): string {
  const key = `modules.catalog.${moduleId}.previews.${commandSummarySlug(command.trigger)}`;
  return orFallback({ key, value: t(key) }, command.preview ?? '');
}

export const PERM_KEYS: Readonly<Record<Perm, MessageKey>> = Object.freeze({
  everyone: 'perm.everyone',
  sub: 'perm.sub',
  vip: 'perm.vip',
  mod: 'perm.mod',
  lead_mod: 'perm.lead_mod',
  broadcaster: 'perm.broadcaster'
});

export const PERM_BADGE_KEYS: Readonly<Record<Perm, MessageKey>> = Object.freeze({
  everyone: 'perm.badge.everyone',
  sub: 'perm.badge.sub',
  vip: 'perm.badge.vip',
  mod: 'perm.badge.mod',
  lead_mod: 'perm.badge.lead_mod',
  broadcaster: 'perm.badge.broadcaster'
});

export function tPerm(t: TFn, perm: Perm): string {
  return t(PERM_KEYS[perm]);
}

export function tPermBadge(t: TFn, perm: Perm): string {
  return t(PERM_BADGE_KEYS[perm]);
}
