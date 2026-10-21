// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  FIELD_RULES,
  TICKET_OPEN_LIMIT_MAX,
  TICKET_OPEN_LIMIT_MIN,
  TICKET_PANEL_DEFAULTS,
  type DiscordConfig,
  type DiscordModuleDef
} from '@bagel/kit';

export type ModuleState = 'on' | 'off' | 'core';

/** `enabled` is undefined when the guild has no row for the module yet. */
export function moduleStateOf(def: Pick<DiscordModuleDef, 'kind'>, enabled: boolean | undefined): ModuleState {
  if (def.kind === 'core') return 'core';
  if (enabled === undefined) return def.kind === 'optin' ? 'off' : 'on';
  return enabled ? 'on' : 'off';
}

export function moduleHrefOf(guildId: string, def: Pick<DiscordModuleDef, 'id' | 'href'>): string {
  return `/discord/${guildId}/${def.href ?? `modules/${def.id}`}`;
}

export type FieldLimit = { min?: number; max?: number };

export function fieldLimit(key: keyof DiscordConfig): FieldLimit | undefined {
  const rule = FIELD_RULES[key];
  if (rule.kind !== 'text' && rule.kind !== 'limit') return undefined;
  return { min: rule.min, max: rule.max };
}

export const PLACEHOLDER_DEFAULTS: Partial<Record<keyof DiscordConfig, string>> = {
  ticketPanelTitle: TICKET_PANEL_DEFAULTS.title,
  ticketPanelBody: TICKET_PANEL_DEFAULTS.body,
  ticketPanelButton: TICKET_PANEL_DEFAULTS.button
};

export const SEGMENTED_NUMBERS: Partial<Record<keyof DiscordConfig, readonly string[]>> = {
  ticketOpenLimit: Array.from({ length: TICKET_OPEN_LIMIT_MAX - TICKET_OPEN_LIMIT_MIN + 1 }, (_, i) => String(TICKET_OPEN_LIMIT_MIN + i))
};
