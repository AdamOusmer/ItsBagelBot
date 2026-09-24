// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { DiscordEntry, DiscordGuildInfo, DiscordLayout, DiscordStatus } from '$lib/server/discord-store';
import { guildBotState, type GuildBotState } from '@bagel/kit';

export type GuildShell = {
  layout: DiscordLayout;
  status: DiscordStatus;
};

export function guildInfoOf(data: GuildShell): DiscordGuildInfo {
  return data.status?.guildPresent ? data.status.guild : (data.layout?.guild ?? data.status.guild);
}

export function botOnlineOf(data: GuildShell): boolean {
  return data.status?.online === true && data.status?.guildPresent === true;
}

export function needsReauthOf(data: GuildShell): boolean {
  return data.status?.needsReauth === true || data.layout?.needsReauth === true;
}

export function pillStateOf(data: GuildShell): GuildBotState {
  return guildBotState({ botPresent: botOnlineOf(data), needsReauth: needsReauthOf(data) });
}

const TEXTLIKE_TYPES = [0, 5];

export function textChannelsOf(layout: DiscordLayout | undefined): DiscordEntry[] {
  return (layout?.channels ?? []).filter((c) => TEXTLIKE_TYPES.includes(c.type));
}

export function voiceChannelsOf(layout: DiscordLayout | undefined): DiscordEntry[] {
  return (layout?.channels ?? []).filter((c) => c.type === 2);
}

export function categoriesOf(layout: DiscordLayout | undefined): DiscordEntry[] {
  return layout?.categories ?? [];
}

export function rolesOf(layout: DiscordLayout | undefined): DiscordEntry[] {
  return (layout?.roles ?? []).filter((r) => r.name !== '@everyone');
}

export function layoutDownOf(layout: DiscordLayout | undefined): boolean {
  return textChannelsOf(layout).length === 0 && rolesOf(layout).length === 0 && categoriesOf(layout).length === 0;
}

export type SinceUnit = { unit: 'minutes' | 'hours' | 'days'; n: number } | null;

export function sinceParts(now: number, sinceMs: number): SinceUnit {
  if (!now || !sinceMs) return null;
  const minutes = Math.max(0, Math.round((now - sinceMs) / 60000));
  if (minutes < 60) return { unit: 'minutes', n: minutes };
  if (minutes < 1440) return { unit: 'hours', n: Math.round(minutes / 60) };
  return { unit: 'days', n: Math.round(minutes / 1440) };
}
