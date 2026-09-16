// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Dashboard copy for a ModuleDef. The catalog files stay English: tests,
// marketing, and the search haystack read them without a locale. The console
// overlays `modules.catalog.{id}.*` from the i18n catalogs. A missing key
// must fall back to the catalog string rather than render the dotted path.
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

export function tModuleLabel(t: TFn, def: Pick<ModuleDef, 'id' | 'label'>): string {
  return tCatalog(t, catalogKey(def.id, 'label'), def.label);
}

export function tModuleTagline(t: TFn, def: Pick<ModuleDef, 'id' | 'tagline'>): string {
  return tCatalog(t, catalogKey(def.id, 'tagline'), def.tagline);
}

export function tModuleDescription(t: TFn, def: Pick<ModuleDef, 'id' | 'description'>): string {
  return tCatalog(t, catalogKey(def.id, 'description'), def.description);
}

export function tModuleFieldPart(
  t: TFn,
  moduleId: string,
  field: ModuleField,
  part: 'label' | 'help' | 'placeholder'
): string {
  const fallback = field[part] ?? '';
  const specific = t(catalogKey(moduleId, 'settings', field.key, part));
  if (specific !== catalogKey(moduleId, 'settings', field.key, part)) return specific;
  const shared = t(catalogKey('shared', field.key, part));
  if (shared !== catalogKey('shared', field.key, part)) return shared;
  return fallback;
}

export function tModuleFieldOption(
  t: TFn,
  moduleId: string,
  fieldKey: string,
  opt: { value: string; label: string }
): string {
  return tCatalog(t, catalogKey(moduleId, 'settings', fieldKey, 'options', opt.value), opt.label);
}

export function tModuleReplyPart(
  t: TFn,
  moduleId: string,
  reply: ModuleReply,
  part: 'label' | 'tagline' | 'event'
): string {
  return tCatalog(t, catalogKey(moduleId, 'replies', reply.key, part), reply[part]);
}

export function commandSummarySlug(trigger: string): string {
  return trigger
    .replace(/^!/, '')
    .replace(/[^a-zA-Z0-9]+/g, '_')
    .replace(/^_|_$/g, '');
}

export function tModuleCommandSummary(t: TFn, moduleId: string, command: ModuleCommandInfo): string {
  return tCatalog(t, catalogKey(moduleId, 'commands', commandSummarySlug(command.trigger)), command.summary);
}

export const PERM_KEYS: Record<Perm, MessageKey> = {
  everyone: 'perm.everyone',
  sub: 'perm.sub',
  vip: 'perm.vip',
  mod: 'perm.mod',
  lead_mod: 'perm.lead_mod',
  broadcaster: 'perm.broadcaster'
};

export const PERM_BADGE_KEYS: Record<Perm, MessageKey> = {
  everyone: 'perm.badge.everyone',
  sub: 'perm.badge.sub',
  vip: 'perm.badge.vip',
  mod: 'perm.badge.mod',
  lead_mod: 'perm.badge.lead_mod',
  broadcaster: 'perm.badge.broadcaster'
};

export function tPerm(t: TFn, perm: Perm): string {
  return t(PERM_KEYS[perm]);
}

export function tPermBadge(t: TFn, perm: Perm): string {
  return t(PERM_BADGE_KEYS[perm]);
}
