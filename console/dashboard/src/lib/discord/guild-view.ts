// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The reads every guild page makes of the shell's load data.
//
// Type-only imports of the store types: the values never leave the server, but
// the shapes are the page contract and re-declaring them here is how they drift.
import type { DiscordEntry, DiscordGuildInfo, DiscordLayout, DiscordStatus } from '$lib/server/discord-store';
import { guildBotState, type GuildBotState } from '@bagel/shared';

export type GuildShell = {
  layout: DiscordLayout;
  status: DiscordStatus;
};

/**
 * Which description of the guild to believe.
 *
 * The status reply describes the guild the GATEWAY currently sees, which is the
 * truth when the bot is in it; the layout reply describes the guild the REST
 * side listed, which is all there is when the bot is not. Preferring status
 * only while `guildPresent` is what stops a name flipping to a stale one every
 * time the gateway drops.
 */
export function guildInfoOf(data: GuildShell): DiscordGuildInfo {
  return data.status?.guildPresent ? data.status.guild : (data.layout?.guild ?? data.status.guild);
}

export function botOnlineOf(data: GuildShell): boolean {
  return data.status?.online === true && data.status?.guildPresent === true;
}

/** Either half can report it: the gateway learns the grant is dead on connect,
 *  the REST layout call learns it on a 403. */
export function needsReauthOf(data: GuildShell): boolean {
  return data.status?.needsReauth === true || data.layout?.needsReauth === true;
}

export function pillStateOf(data: GuildShell): GuildBotState {
  return guildBotState({ botPresent: botOnlineOf(data), needsReauth: needsReauthOf(data) });
}

// Discord channel types: 0 text, 2 voice, 5 announcement. Categories arrive as
// their own list from outgress rather than being sieved out of channels by
// type.
//
// Type 5 belongs in every text picker: an announcement channel takes the same
// message a text channel does, and #announcements is the single most likely
// place a streamer wants go-live posts. Filtering on type === 0 alone meant
// that channel simply was not in the list, with nothing on screen saying why.
// It is labelled rather than silently mixed in because it behaves differently
// once posted to -- Discord rate-limits it hard and fans it out to every
// following server.
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

/** Each picker asks its own list, so a reply that carried channels but no roles
 *  disables the role pickers alone instead of the whole page. This is the case
 *  where NOTHING came back. */
export function layoutDownOf(layout: DiscordLayout | undefined): boolean {
  return textChannelsOf(layout).length === 0 && rolesOf(layout).length === 0 && categoriesOf(layout).length === 0;
}

/**
 * Uptime, in whole units, from a clock the caller owns.
 *
 * `now` is passed in rather than read here because an uptime rendered during
 * SSR is already stale when it lands and hydration reports the mismatch: the
 * pages hold `now` at 0 until the browser sets it, and 0 renders no label at
 * all.
 */
export type SinceUnit = { unit: 'minutes' | 'hours' | 'days'; n: number } | null;

export function sinceParts(now: number, sinceMs: number): SinceUnit {
  if (!now || !sinceMs) return null;
  const minutes = Math.max(0, Math.round((now - sinceMs) / 60000));
  if (minutes < 60) return { unit: 'minutes', n: minutes };
  if (minutes < 1440) return { unit: 'hours', n: Math.round(minutes / 60) };
  return { unit: 'days', n: Math.round(minutes / 1440) };
}
