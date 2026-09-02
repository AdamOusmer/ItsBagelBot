// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Discord module blob + outgress setup RPC. Channel/role snowflakes live in
// the module row sesame and outgress both read; the bot token never does.
import { rpc } from '@bagel/shared/server/nats';
import { MOD } from '@bagel/shared';
import { SUB } from './services';
import { listModules, upsertModule } from './commands-store';

const DISCORD_MODULE = MOD.discord;

export type DiscordConfig = {
  guildId: string;
  liveChannelId: string;
  clipsChannelId: string;
  welcomeChannelId: string;
  alertsChannelId: string;
  voiceHubId: string;
  liveRoleId: string;
  modsRoleId: string;
  regularsRoleId: string;
  memberRoleId: string;
  liveEnabled: string;
  clipsEnabled: string;
  raidEnabled: string;
  giftEnabled: string;
  cheerEnabled: string;
  subMilestoneEnabled: string;
  welcomeEnabled: string;
  goodbyeEnabled: string;
  voiceEnabled: string;
  giftMin: string;
  cheerMin: string;
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

const EMPTY: DiscordConfig = {
  guildId: '',
  liveChannelId: '',
  clipsChannelId: '',
  welcomeChannelId: '',
  alertsChannelId: '',
  voiceHubId: '',
  liveRoleId: '',
  modsRoleId: '',
  regularsRoleId: '',
  memberRoleId: '',
  liveEnabled: '',
  clipsEnabled: '',
  raidEnabled: '',
  giftEnabled: '',
  cheerEnabled: '',
  subMilestoneEnabled: '',
  welcomeEnabled: '',
  goodbyeEnabled: '',
  voiceEnabled: '',
  giftMin: '',
  cheerMin: '',
  categoryAllow: '',
  categoryDeny: '',
  twitchLogin: '',
  streamerDiscordId: ''
};

function asRecord(raw: unknown): Record<string, string> {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {};
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

export async function readDiscord(userId: string): Promise<DiscordView> {
  const rows = await listModules(userId);
  const row = rows.find((r) => r.name === DISCORD_MODULE);
  const config = parseConfig(row?.configs);
  return {
    enabled: row ? row.is_enabled : false,
    connected: config.guildId.trim() !== '',
    config
  };
}

export async function saveDiscord(
  userId: string,
  enabled: boolean,
  config: DiscordConfig
): Promise<void> {
  await upsertModule(userId, DISCORD_MODULE, enabled, config);
}

type SetupReply = {
  guild_id?: string;
  live_channel_id?: string;
  clips_channel_id?: string;
  welcome_channel_id?: string;
  alerts_channel_id?: string;
  voice_hub_id?: string;
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

// setupGuild asks outgress to fill the community template (or refuse a
// lived-in server) and always binds the guild→Twitch reverse index.
export async function setupGuild(
  userId: string,
  guildId: string,
  current: DiscordConfig
): Promise<DiscordSetup> {
  const r = await rpc<SetupReply>(
    `${SUB.outgressRpc}.discord.setup`,
    { user_id: userId, guild_id: guildId },
    15000
  );
  if (r.error) return { config: current, refused: '', error: r.error };
  const next: DiscordConfig = {
    ...current,
    guildId: r.guild_id || guildId,
    liveChannelId: r.live_channel_id || current.liveChannelId,
    clipsChannelId: r.clips_channel_id || current.clipsChannelId,
    welcomeChannelId: r.welcome_channel_id || current.welcomeChannelId,
    alertsChannelId: r.alerts_channel_id || current.alertsChannelId,
    voiceHubId: r.voice_hub_id || current.voiceHubId,
    liveRoleId: r.live_role_id || current.liveRoleId,
    modsRoleId: r.mods_role_id || current.modsRoleId,
    regularsRoleId: r.regulars_role_id || current.regularsRoleId,
    memberRoleId: r.member_role_id || current.memberRoleId
  };
  return { config: next, refused: r.refused ?? '', error: '' };
}

export function blankDiscordConfig(): DiscordConfig {
  return { ...EMPTY };
}
