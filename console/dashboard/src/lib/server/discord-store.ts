// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Discord module blob + outgress RPCs. Channel/role snowflakes live in the
// module row sesame and outgress both read; the bot token never does.
//
// The blob's shape, defaults and validation live in @bagel/shared's
// discord-config so they can be unit-tested (the console runner only executes
// shared/**); this file is the transport half.
import { rpc } from '@bagel/shared/server/nats';
import {
  MOD,
  encodePinnedRoles,
  parseDiscordConfig,
  parsePinnedRoles,
  type DiscordConfig,
  type PinnedRoles
} from '@bagel/shared';
import { SUB } from './services';
import { listModules, upsertModule } from './commands-store';

export {
  blankDiscordConfig,
  type DiscordConfig
} from '@bagel/shared';

const DISCORD_MODULE = MOD.discord;

// SETUP_TIMEOUT_MS sits above outgress's 45 s setup handler: a full fill is
// ~24 sequential Discord creates plus their Retry-After waits.
const SETUP_TIMEOUT_MS = 60000;
const LAYOUT_TIMEOUT_MS = 12000;
// Status is a Valkey read plus one cached GetGuild; anything slower than this
// is an outage, and the page renders an offline pill rather than blocking the
// whole load behind it.
const STATUS_TIMEOUT_MS = 3000;
// A repost deletes the old panel message and posts a new one: two Discord
// calls that can each eat a Retry-After.
const REPOST_TIMEOUT_MS = 10000;
const UNBIND_TIMEOUT_MS = 5000;

/**
 * The refusal codes every dingress reply now carries.
 *
 * Before this the console matched substrings of an English error message,
 * which broke the moment outgress reworded one and could never be localised.
 * `code` is the contract; `messageCode` below still reads the old text so a
 * console deployed ahead of outgress keeps recognising the one refusal that
 * actually changes what the page renders. Drop messageCode once outgress has
 * shipped codes for one release.
 */
export const DISCORD_CODES = [
  'bound_elsewhere',
  'not_bound',
  'discord_unavailable',
  'forbidden',
  'rate_limited',
  'invalid'
] as const;

export type DiscordCode = (typeof DISCORD_CODES)[number] | '';

const BOUND_ELSEWHERE_TEXT = 'already linked to another Twitch channel';

const CODE_SET: ReadonlySet<string> = new Set(DISCORD_CODES);

type CodedReply = { code?: string; error?: string };

export function replyCode(r: CodedReply): DiscordCode {
  const code = (r.code ?? '').trim();
  if (CODE_SET.has(code)) return code as DiscordCode;
  return messageCode(r.error ?? '');
}

function messageCode(error: string): DiscordCode {
  if (error.includes(BOUND_ELSEWHERE_TEXT)) return 'bound_elsewhere';
  return '';
}

export type DiscordView = {
  enabled: boolean;
  connected: boolean;
  config: DiscordConfig;
};

export type DiscordEntry = { id: string; name: string; type: number };

export type DiscordGuildInfo = { id: string; name: string; iconUrl: string; memberCount: number };

// needsReauth is true when this guild's bot role predates CHANGE_NICKNAME,
// so the premium per-guild rename is refused while the avatar still applies.
// Discord freezes a bot's permissions at install, so the only fix is the
// streamer re-authorizing; outgress learns it from Discord's own 403 and
// clears it the first time a rename succeeds.
export type DiscordLayout = {
  channels: DiscordEntry[];
  categories: DiscordEntry[];
  roles: DiscordEntry[];
  guild: DiscordGuildInfo;
  needsReauth: boolean;
  botOnline: boolean;
  botSinceMs: number;
  lastCloseCode: number;
};

// The gateway's own view of itself, written by ingress on every transition
// (see the bot status key contract) and read back here for the status card.
export type DiscordStatus = {
  online: boolean;
  sinceMs: number;
  sessionResumes: number;
  guildPresent: boolean;
  guild: DiscordGuildInfo;
  needsReauth: boolean;
  lastCloseCode: number;
  code: DiscordCode;
  error: string;
};

export function blankGuildInfo(): DiscordGuildInfo {
  return { id: '', name: '', iconUrl: '', memberCount: 0 };
}

export function blankLayout(): DiscordLayout {
  return {
    channels: [],
    categories: [],
    roles: [],
    guild: blankGuildInfo(),
    needsReauth: false,
    botOnline: false,
    botSinceMs: 0,
    lastCloseCode: 0
  };
}

export function blankStatus(): DiscordStatus {
  return {
    online: false,
    sinceMs: 0,
    sessionResumes: 0,
    guildPresent: false,
    guild: blankGuildInfo(),
    needsReauth: false,
    lastCloseCode: 0,
    code: '',
    error: ''
  };
}

export type DiscordUser = { userId: string };

// subscribers mirrors the streamer's subscriber toggle at the moment setup
// runs. The fill skips the Subscriber role and its locked category when it is
// off, so a server that does not use the tier never grows a category nobody
// can open. pinnedRoles tells the fill to ADOPT an existing guild role for a
// slot instead of creating or renaming one by name.
export type DiscordGuildTarget = {
  userId: string;
  guildId: string;
  subscribers?: boolean;
  pinnedRoles?: PinnedRoles;
};

export type DiscordSave = { userId: string; enabled: boolean; config: DiscordConfig };

export async function readDiscord(user: DiscordUser): Promise<DiscordView> {
  const rows = await listModules(user.userId);
  const row = rows.find((r) => r.name === DISCORD_MODULE);
  const config = parseDiscordConfig(row?.configs);
  return {
    enabled: row ? row.is_enabled : false,
    connected: config.guildId.trim() !== '',
    config
  };
}

export async function saveDiscord(save: DiscordSave): Promise<void> {
  await upsertModule(save.userId, DISCORD_MODULE, save.enabled, save.config);
}

type SetupReply = CodedReply & {
  guild_id?: string;
  live_channel_id?: string;
  clips_channel_id?: string;
  welcome_channel_id?: string;
  voice_hub_id?: string;
  log_channel_id?: string;
  ticket_channel_id?: string;
  ticket_category_id?: string;
  ticket_archive_category_id?: string;
  subs_channel_id?: string;
  subs_category_id?: string;
  vip_channel_id?: string;
  vip_category_id?: string;
  owner_role_id?: string;
  vip_role_id?: string;
  subscriber_role_id?: string;
  lead_mod_role_id?: string;
  mods_role_id?: string;
  regulars_role_id?: string;
  member_role_id?: string;
  refused?: string;
};

export type DiscordSetup = {
  config: DiscordConfig;
  refused: string;
  error: string;
  code: DiscordCode;
};

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
  ['ticketArchiveCategoryId', 'ticket_archive_category_id'],
  ['subsChannelId', 'subs_channel_id'],
  ['subsCategoryId', 'subs_category_id'],
  ['vipChannelId', 'vip_channel_id'],
  ['vipCategoryId', 'vip_category_id'],
  ['ownerRoleId', 'owner_role_id'],
  ['vipRoleId', 'vip_role_id'],
  ['subscriberRoleId', 'subscriber_role_id'],
  ['leadModRoleId', 'lead_mod_role_id'],
  ['modsRoleId', 'mods_role_id'],
  ['regularsRoleId', 'regulars_role_id'],
  ['memberRoleId', 'member_role_id']
];

// setupGuild asks outgress to fill the community template (or, on a lived-in
// server, adopt the channels it recognises by name) and bind the
// guild→Twitch reverse index. Outgress refuses a guild bound to someone else.
export async function setupGuild(
  target: DiscordGuildTarget,
  current: DiscordConfig
): Promise<DiscordSetup> {
  const r = await rpc<SetupReply>(
    `${SUB.dingressRpc}.discord.setup`,
    {
      user_id: target.userId,
      guild_id: target.guildId,
      subscribers: target.subscribers === true,
      pinned_roles: target.pinnedRoles ?? {}
    },
    SETUP_TIMEOUT_MS
  );
  if (r.error) return { config: current, refused: '', error: r.error, code: replyCode(r) };
  return { config: applySetup(current, target.guildId, r), refused: r.refused ?? '', error: '', code: '' };
}

function applySetup(current: DiscordConfig, guildId: string, r: SetupReply): DiscordConfig {
  const next: DiscordConfig = { ...current, guildId };
  for (const [key, replyKey] of SETUP_FIELDS) {
    const v = r[replyKey];
    if (typeof v !== 'string') continue;
    if (!v) continue;
    next[key] = v;
  }
  return next;
}

type WireEntry = { id?: string; name?: string; type?: number };
type WireGuild = { id?: string; name?: string; icon_url?: string; member_count?: number };

type LayoutReply = CodedReply & {
  channels?: WireEntry[];
  categories?: WireEntry[];
  roles?: WireEntry[];
  guild?: WireGuild;
  needs_reauth?: boolean;
  bot_online?: boolean;
  bot_since_unix_ms?: number;
  last_close_code?: number;
};

function entries(list: WireEntry[] | undefined): DiscordEntry[] {
  return (list ?? [])
    .filter((e) => typeof e.id === 'string' && e.id !== '')
    .map((e) => ({ id: String(e.id), name: e.name ?? '', type: e.type ?? 0 }));
}

function guildInfo(g: WireGuild | undefined): DiscordGuildInfo {
  return {
    id: g?.id ?? '',
    name: g?.name ?? '',
    iconUrl: g?.icon_url ?? '',
    memberCount: Number(g?.member_count ?? 0)
  };
}

// guildLayout lists the bound guild's channels, categories and roles for the
// pickers. Categories arrive as their own list rather than being filtered out
// of channels by type: the two pickers mean different things, and a reply that
// omits categories should show an empty category picker, not silently reuse
// whatever type-4 rows happened to be in `channels`.
export async function guildLayout(target: DiscordGuildTarget): Promise<DiscordLayout> {
  const r = await rpc<LayoutReply>(
    `${SUB.dingressRpc}.discord.layout`,
    { user_id: target.userId, guild_id: target.guildId },
    LAYOUT_TIMEOUT_MS
  );
  if (r.error) throw new Error(r.error);
  return {
    channels: entries(r.channels),
    categories: entries(r.categories),
    roles: entries(r.roles),
    guild: guildInfo(r.guild),
    needsReauth: r.needs_reauth === true,
    botOnline: r.bot_online === true,
    botSinceMs: Number(r.bot_since_unix_ms ?? 0),
    lastCloseCode: Number(r.last_close_code ?? 0)
  };
}

type StatusReply = CodedReply & {
  online?: boolean;
  since_unix_ms?: number;
  session_resumes?: number;
  guild_present?: boolean;
  guild_name?: string;
  icon_url?: string;
  member_count?: number;
  needs_reauth?: boolean;
  last_close_code?: number;
};

// botStatus answers "is the bot actually in there right now": the gateway's
// own status key plus a member count. It never throws — an unreachable
// outgress is itself the answer the status card renders.
export async function botStatus(target: DiscordGuildTarget): Promise<DiscordStatus> {
  const r = await rpc<StatusReply>(
    `${SUB.dingressRpc}.discord.status`,
    { user_id: target.userId, guild_id: target.guildId },
    STATUS_TIMEOUT_MS
  );
  return {
    online: r.online === true,
    sinceMs: Number(r.since_unix_ms ?? 0),
    sessionResumes: Number(r.session_resumes ?? 0),
    guildPresent: r.guild_present === true,
    guild: {
      id: target.guildId,
      name: r.guild_name ?? '',
      iconUrl: r.icon_url ?? '',
      memberCount: Number(r.member_count ?? 0)
    },
    needsReauth: r.needs_reauth === true,
    lastCloseCode: Number(r.last_close_code ?? 0),
    code: replyCode(r),
    error: r.error ?? ''
  };
}

export type DiscordRepost = { messageId: string; error: string; code: DiscordCode };

// repostDesk deletes the remembered ticket panel message and posts a fresh one
// from the saved config. Called after the embed editor saves, because Discord
// gives no way to edit a message the bot posted in a previous session's
// interaction context.
export async function repostDesk(target: DiscordGuildTarget): Promise<DiscordRepost> {
  const r = await rpc<CodedReply & { message_id?: string }>(
    `${SUB.dingressRpc}.discord.desk.repost`,
    { user_id: target.userId, guild_id: target.guildId },
    REPOST_TIMEOUT_MS
  );
  return { messageId: r.message_id ?? '', error: r.error ?? '', code: replyCode(r) };
}

// unbindGuild drops the guild→Twitch reverse index on disconnect so outgress
// stops resolving the guild to this broadcaster.
export async function unbindGuild(target: DiscordGuildTarget): Promise<void> {
  const r = await rpc<CodedReply>(
    `${SUB.dingressRpc}.discord.unbind`,
    { user_id: target.userId, guild_id: target.guildId },
    UNBIND_TIMEOUT_MS
  );
  if (r.error) throw new Error(r.error);
}

// pinnedRolesOf reads the slot→role pins the streamer chose in the dashboard
// so setup adopts them instead of creating a role by name. Round-tripped
// through the shared encoder so an unknown slot or a malformed id can never
// reach outgress, whatever is in the stored blob.
export function pinnedRolesOf(config: DiscordConfig): PinnedRoles {
  return parsePinnedRoles(encodePinnedRoles(parsePinnedRoles(config.pinnedRoles)));
}
