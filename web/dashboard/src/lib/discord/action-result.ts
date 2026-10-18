// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { actionPayload, discordModuleById, tDiscordModuleField, type ActionOk, type I18n } from '@bagel/kit';
import { DISCORD_CODE_KEYS } from '../discord-messages';

export type ActionPayload = ActionOk & {
  code?: string;
  refused?: string;
  fields?: string[];
  version?: number;
  enabled?: boolean;
};

export function payloadOf(result: unknown): ActionPayload | undefined {
  return actionPayload<ActionPayload>(result);
}

export function succeeded(result: { type: string }, p: ActionPayload | undefined): boolean {
  return result.type === 'success' && p?.ok !== false;
}

export function fieldLabelOf(t: I18n['t'], moduleId: string, key: string): string {
  const field = discordModuleById(moduleId)?.fields.find((f) => f.key === key);
  return field ? tDiscordModuleField(t, moduleId, field, 'label') : key;
}

export function refusalTextOf(
  t: I18n['t'],
  p: ActionPayload | undefined,
  fallback: string,
  moduleId = ''
): string {
  if (p?.code === 'invalid' && p.fields?.length) {
    return t('discord.errors.invalidFields', { fields: p.fields.map((f) => fieldLabelOf(t, moduleId, f)).join(', ') });
  }
  const key = p?.code ? DISCORD_CODE_KEYS[p.code] : undefined;
  if (key) return t(key);
  return fallback || (p?.error ?? '');
}
