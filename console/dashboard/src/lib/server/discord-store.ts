// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Discord module blob + outgress RPCs. Channel/role snowflakes live in the
// module row sesame and outgress both read; the bot token never does.
import { rpc } from '@bagel/shared/server/nats';
import { MOD } from '@bagel/shared';
import { SUB } from './services';
import { listModules, upsertModule } from './commands-store';

const DISCORD_MODULE = MOD.discord;

// SETUP_TIMEOUT_MS sits above outgress's 45 s setup handler: a full fill is
// ~24 sequential Discord creates plus their Retry-After waits.
const SETUP_TIMEOUT_MS = 60000;
const LAYOUT_TIMEOUT_MS = 12000;

const BOUND_ELSEWHERE = 'already linked to another Twitch channel';

export type DiscordConfig = {
  guildId: string;
  liveChannelId: string;
  clipsChannelId: string;
  welcomeChannelId: string;
  voiceHubId: string;
  liveRoleId: string;
  modsRoleId: string;
  regularsRoleId: string;
  memberRoleId: string;
  logChannelId: string;
  ticketChannelId: string;
  ticketCategoryId: string;
  liveEnabled: string;
  clipsEnabled: string;
  welcomeEnabled: string;
  goodbyeEnabled: string;
  voiceEnabled: string;
  ticketsEnabled: string;
  logsEnabled: string;
  levelsEnabled: string;
  categoryAllow: string;
  categoryDeny: string;
  twitchLogin: string;
  streamerDiscordId: string;
};

export type DiscordView = {
  enabled: boolean;
  connected: boolean;
  config: DiscordConfig;
};

export type DiscordEntry = { id: string; name: string; type: number };

export type DiscordLayout = { channels: DiscordEntry[]; roles: DiscordEntry[] };

const EMPTY: DiscordConfig = {
  guildId: '',
  liveChannelId: '',
  clipsChannelId: '',
  welcomeChannelId: '',
  voiceHubId: '',
  liveRoleId: '',
  modsRoleId: '',
  regularsRoleId: '',
  memberRoleId: '',
  logChannelId: '',
  ticketChannelId: '',
  ticketCategoryId: '',
  liveEnabled: '',
  clipsEnabled: '',
  welcomeEnabled: '',
  goodbyeEnabled: '',
  voiceEnabled: '',
  ticketsEnabled: '',
  logsEnabled: '',
  levelsEnabled: '',
  categoryAllow: '',
  categoryDeny: '',
  twitchLogin: '',
  streamerDiscordId: ''
};

function asRecord(raw: unknown): Record<string, string> {
  if (raw == null) return {};
  if (typeof raw !== 'object') return {};
  if (Array.isArray(raw)) return {};
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
    if (typeof v === 'string') out[k] = v;
  }
  return out;
}

function parseConfig(raw: unknown): DiscordConfig {
  const src = asRecord(raw);
  const out = { ...EMPTY };
  for (const key of Object.keys(EMPTY) as (keyof DiscordConfig)[]) {
    if (src[key] !== undefined) out[key] = src[key];
  }
  return out;
}

export type DiscordUser = { userId: string };

export type DiscordGuildTarget = { userId: string; guildId: string };

export type DiscordSave = { userId: string; enabled: boolean; config: DiscordConfig };

export async function readDiscord(user: DiscordUser): Promise<DiscordView> {
  const rows = await listModules(user.userId);
  const row = rows.find((r) => r.name === DISCORD_MODULE);
  const config = parseConfig(row?.configs);
  return {
    enabled: row ? row.is_enabled : false,
    connected: config.guildId.trim() !== '',
    config
  };
}

export async function saveDiscord(save: DiscordSave): Promise<void> {
  await upsertModule(save.userId, DISCORD_MODULE, save.enabled, save.config);
}

type SetupReply = {
  guild_id?: string;
  live_channel_id?: string;
  clips_channel_id?: string;
  welcome_channel_id?: string;
  voice_hub_id?: string;
  log_channel_id?: string;
  ticket_channel_id?: string;
  ticket_category_id?: string;
  live_role_id?: string;
  mods_role_id?: string;
  regulars_role_id?: string;
  member_role_id?: string;
  refused?: string;
  error?: string;
};

export type DiscordSetup = {
  config: DiscordConfig;
  refused: string;
  error: string;
};

// isBoundElsewhere recognises outgress's refusal of a guild already linked
// to a different broadcaster.
export function isBoundElsewhere(result: { error: string }): boolean {
  return result.error.includes(BOUND_ELSEWHERE);
}

// setupGuild asks outgress to fill the community template (or, on a lived-in
// server, adopt the channels it recognises by name) and bind the
// guild→Twitch reverse index. Outgress refuses a guild bound to someone else.
export async function setupGuild(
  target: DiscordGuildTarget,
  current: DiscordConfig
): Promise<DiscordSetup> {
  const r = await rpc<SetupReply>(
    `${SUB.dingressRpc}.discord.setup`,
    { user_id: target.userId, guild_id: target.guildId },
    SETUP_TIMEOUT_MS
  );
  if (r.error) return { config: current, refused: '', error: r.error };
  const next: DiscordConfig = { ...current, guildId: target.guildId };
  for (const [key, replyKey] of SETUP_FIELDS) {
    const v = r[replyKey];
    if (typeof v !== 'string') continue;
    if (!v) continue;
    next[key] = v;
  }
  return { config: next, refused: r.refused ?? '', error: '' };
}

// SETUP_FIELDS maps the reply's snowflakes onto the module blob; a field the
// fill did not produce keeps its current value.
const SETUP_FIELDS: [keyof DiscordConfig, keyof SetupReply][] = [
  ['guildId', 'guild_id'],
  ['liveChannelId', 'live_channel_id'],
  ['clipsChannelId', 'clips_channel_id'],
  ['welcomeChannelId', 'welcome_channel_id'],
  ['voiceHubId', 'voice_hub_id'],
  ['logChannelId', 'log_channel_id'],
  ['ticketChannelId', 'ticket_channel_id'],
  ['ticketCategoryId', 'ticket_category_id'],
  ['liveRoleId', 'live_role_id'],
  ['modsRoleId', 'mods_role_id'],
  ['regularsRoleId', 'regulars_role_id'],
  ['memberRoleId', 'member_role_id']
];

type LayoutReply = {
  channels?: { id: string; name: string; type?: number }[];
  roles?: { id: string; name: string; type?: number }[];
  error?: string;
};

function entries(list: LayoutReply['channels']): DiscordEntry[] {
  return (list ?? []).map((e) => ({ id: e.id, name: e.name, type: e.type ?? 0 }));
}

// guildLayout lists the bound guild's channels and roles for the pickers.
export async function guildLayout(target: DiscordGuildTarget): Promise<DiscordLayout> {
  const r = await rpc<LayoutReply>(
    `${SUB.dingressRpc}.discord.layout`,
    { user_id: target.userId, guild_id: target.guildId },
    LAYOUT_TIMEOUT_MS
  );
  if (r.error) throw new Error(r.error);
  return { channels: entries(r.channels), roles: entries(r.roles) };
}

// unbindGuild drops the guild→Twitch reverse index on disconnect so outgress
// stops resolving the guild to this broadcaster.
export async function unbindGuild(target: DiscordGuildTarget): Promise<void> {
  const r = await rpc<{ error?: string }>(
    `${SUB.dingressRpc}.discord.unbind`,
    { user_id: target.userId, guild_id: target.guildId },
    5000
  );
  if (r.error) throw new Error(r.error);
}

export function blankDiscordConfig(): DiscordConfig {
  return { ...EMPTY };
}
