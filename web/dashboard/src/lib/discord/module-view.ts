// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  TICKET_PANEL_BODY_MAX,
  TICKET_PANEL_BUTTON_MAX,
  TICKET_PANEL_DEFAULTS,
  TICKET_PANEL_TITLE_MAX,
  VOICE_NAME_MAX,
  VOICE_USER_LIMIT_MAX,
  VOICE_USER_LIMIT_MIN,
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

export type FieldLimit = { min?: number; max: number };

export const FIELD_LIMITS: Partial<Record<keyof DiscordConfig, FieldLimit>> = {
  voiceNameTemplate: { max: VOICE_NAME_MAX },
  voiceUserLimit: { min: VOICE_USER_LIMIT_MIN, max: VOICE_USER_LIMIT_MAX },
  ticketPanelTitle: { max: TICKET_PANEL_TITLE_MAX },
  ticketPanelBody: { max: TICKET_PANEL_BODY_MAX },
  ticketPanelButton: { max: TICKET_PANEL_BUTTON_MAX }
};

export const PLACEHOLDER_DEFAULTS: Partial<Record<keyof DiscordConfig, string>> = {
  ticketPanelTitle: TICKET_PANEL_DEFAULTS.title,
  ticketPanelBody: TICKET_PANEL_DEFAULTS.body,
  ticketPanelButton: TICKET_PANEL_DEFAULTS.button
};

export const SEGMENTED_NUMBERS: Partial<Record<keyof DiscordConfig, readonly string[]>> = {
  ticketOpenLimit: ['1', '2', '3', '4', '5']
};
