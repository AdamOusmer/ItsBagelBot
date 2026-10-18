// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpcReply } from '@bagel/kit/server/nats';
import { codeReader, type CodedReply } from '@bagel/kit/server/rpc-code';
import {
  MOD,
  droppedPinNotice,
  encodePinnedRoles,
  parseConfigVersion,
  parseDiscordConfig,
  parseDiscordModuleSettings,
  parsePinnedRoles,
  ticketPanelPayload,
  type DiscordConfig,
  type DiscordModuleSettings,
  type PinnedRoles,
  type PinnedSlot
} from '@bagel/kit';
import { SUB } from './services';
import { upsertModule } from './commands-store';
import { readModuleBlob } from './module-blob';

export {
  blankDiscordConfig,
  type DiscordConfig
} from '@bagel/kit';

const DISCORD_MODULE = MOD.discord;

// Must exceed outgress's 45 s setup handler.
const SETUP_TIMEOUT_MS = 60000;
const LAYOUT_TIMEOUT_MS = 12000;
const STATUS_TIMEOUT_MS = 3000;
const REPOST_TIMEOUT_MS = 10000;
const UNBIND_TIMEOUT_MS = 5000;
const CONFIG_GET_TIMEOUT_MS = 2000;
const CONFIG_SET_TIMEOUT_MS = 5000;
const MODULE_LIST_TIMEOUT_MS = 2000;
const MODULE_SET_TIMEOUT_MS = 5000;
const GUILDS_TIMEOUT_MS = 8000;

// Keep timeout, not_found and unknown listed: an unlisted code reads as '' (success).
export const DISCORD_CODES = [
  'bound_elsewhere',
  'conflict',
  'not_bound',
  'discord_unavailable',
  'forbidden',
  'rate_limited',
  'invalid',
  'timeout',
  'not_found',
  'unknown'
] as const;

export type DiscordCode = (typeof DISCORD_CODES)[number] | '';

export const replyCode: (r: CodedReply) => DiscordCode = codeReader(DISCORD_CODES);

export class DiscordRefusal extends Error {
  readonly code: DiscordCode;

  constructor(code: DiscordCode, message: string) {
    super(message);
    this.name = 'DiscordRefusal';
    this.code = code;
  }
}

export function refusalCode(err: unknown): DiscordCode {
  return err instanceof DiscordRefusal ? err.code : '';
}

export type DiscordView = {
  enabled: boolean;
  twitchLogin: string;
  guilds: DiscordGuildSummary[];
  truncated: boolean;
};

export type DiscordGuildSummary = {
  guildId: string;
  name: string;
  iconUrl: string;
  memberCount: number;
  botPresent: boolean;
  needsReauth: boolean;
  reauthUnknown: boolean;
  boundAtMs: number;
};

export type DiscordGuildConfig = {
  config: DiscordConfig;
  version: number;
  found: boolean;
};

export type DiscordEntry = {
  id: string;
  name: string;
  type: number;
  parentName?: string;
  botCanSend?: boolean;
  botCanEmbed?: boolean;
};

export type DiscordGuildInfo = { id: string; name: string; iconUrl: string; memberCount: number };

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

export type DiscordGuildTarget = {
  userId: string;
  guildId: string;
  subscribers?: boolean;
  pinnedRoles?: PinnedRoles;
  installedBy?: string;
  mode?: 'apply' | 'adopt';
};

export type DiscordSave = { userId: string; enabled: boolean; twitchLogin: string };

export async function readDiscord(user: DiscordUser): Promise<DiscordView> {
  const { enabled, configs } = await readModuleBlob<unknown>(user.userId, DISCORD_MODULE);
  const list = await listGuildsPage(user);
  return {
    enabled,
    twitchLogin: parseDiscordConfig(configs).twitchLogin,
    guilds: list.guilds,
    truncated: list.truncated
  };
}

export async function readLegacyBlob(user: DiscordUser): Promise<DiscordConfig> {
  const { configs } = await readModuleBlob<unknown>(user.userId, DISCORD_MODULE);
  return parseDiscordConfig(configs);
}

/** Destructive: callers must write channel and role ids onto the guild row first. */
export async function saveDiscordModule(save: DiscordSave): Promise<void> {
  await upsertModule(save.userId, DISCORD_MODULE, save.enabled, { twitchLogin: save.twitchLogin });
}

type GuildsReply = CodedReply & {
  truncated?: boolean;
  guilds?: {
    guild_id?: string;
    name?: string;
    icon_url?: string;
    member_count?: number;
    bot_present?: boolean;
    needs_reauth?: boolean;
    reauth_unknown?: boolean;
    bound_at_unix_ms?: number;
  }[];
};

export type DiscordGuildList = { guilds: DiscordGuildSummary[]; truncated: boolean };

export async function listGuildsPage(user: DiscordUser): Promise<DiscordGuildList> {
  const r = await rpcReply<GuildsReply>(
    `${SUB.dingressRpc}.discord.guilds.list`,
    { user_id: user.userId },
    GUILDS_TIMEOUT_MS
  );
  if (r.error) throw new Error(r.error);
  return {
    guilds: (r.guilds ?? [])
      .filter((g) => typeof g.guild_id === 'string' && g.guild_id !== '')
      .map((g) => ({
        guildId: String(g.guild_id),
        name: g.name ?? '',
        iconUrl: g.icon_url ?? '',
        memberCount: Number(g.member_count ?? 0),
        botPresent: g.bot_present === true,
        needsReauth: g.needs_reauth === true,
        reauthUnknown: g.reauth_unknown === true,
        boundAtMs: Number(g.bound_at_unix_ms ?? 0)
      })),
    truncated: r.truncated === true
  };
}

export async function listGuilds(user: DiscordUser): Promise<DiscordGuildSummary[]> {
  return (await listGuildsPage(user)).guilds;
}

type ConfigGetReply = CodedReply & { config?: unknown; version?: unknown; found?: boolean };

export async function readGuildConfig(target: DiscordGuildTarget): Promise<DiscordGuildConfig> {
  const r = await rpcReply<ConfigGetReply>(
    `${SUB.dingressRpc}.discord.config.get`,
    { user_id: target.userId, guild_id: target.guildId },
    CONFIG_GET_TIMEOUT_MS
  );
  if (r.error) throw new DiscordRefusal(replyCode(r), r.error);
  return {
    config: { ...parseDiscordConfig(r.config), guildId: target.guildId },
    version: parseConfigVersion(r.version),
    found: r.found === true
  };
}

export type DiscordGuildSave = DiscordGuildTarget & { config: DiscordConfig; expectedVersion: number };

export type DiscordSaveResult = { version: number; code: DiscordCode; error: string };

export async function saveGuildConfig(save: DiscordGuildSave): Promise<DiscordSaveResult> {
  const r = await rpcReply<CodedReply & { version?: unknown }>(
    `${SUB.dingressRpc}.discord.config.set`,
    {
      user_id: save.userId,
      guild_id: save.guildId,
      config: save.config,
      expected_version: save.expectedVersion
    },
    CONFIG_SET_TIMEOUT_MS
  );
  return { version: parseConfigVersion(r.version), code: replyCode(r), error: r.error ?? '' };
}

export type DiscordModuleRow = { enabled: boolean; settings: DiscordModuleSettings; version: number };

export type DiscordGuildModules = { modules: Record<string, DiscordModuleRow>; found: boolean };

type ModuleListReply = CodedReply & {
  modules?: { name?: unknown; enabled?: unknown; settings?: unknown; version?: unknown }[];
  found?: boolean;
};

function moduleSettingsBlob(raw: unknown): unknown {
  if (typeof raw !== 'string') return raw;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

export async function listGuildModules(target: DiscordGuildTarget): Promise<DiscordGuildModules> {
  const r = await rpcReply<ModuleListReply>(
    `${SUB.dingressRpc}.discord.module.list`,
    { user_id: target.userId, guild_id: target.guildId },
    MODULE_LIST_TIMEOUT_MS
  );
  if (r.error) throw new DiscordRefusal(replyCode(r), r.error);
  const modules: Record<string, DiscordModuleRow> = {};
  for (const row of r.modules ?? []) {
    if (typeof row.name !== 'string' || row.name === '') continue;
    modules[row.name] = {
      enabled: row.enabled === true,
      settings: parseDiscordModuleSettings(row.name, moduleSettingsBlob(row.settings)),
      version: parseConfigVersion(row.version)
    };
  }
  return { modules, found: r.found === true };
}

export type DiscordModuleSave = DiscordGuildTarget & {
  name: string;
  enabled: boolean;
  settings: DiscordModuleSettings;
  expectedVersion: number;
};

export type DiscordModuleSaveResult = DiscordSaveResult & { fields: string[] };

export async function setGuildModule(save: DiscordModuleSave): Promise<DiscordModuleSaveResult> {
  const r = await rpcReply<CodedReply & { version?: unknown; fields?: unknown }>(
    `${SUB.dingressRpc}.discord.module.set`,
    {
      user_id: save.userId,
      guild_id: save.guildId,
      name: save.name,
      enabled: save.enabled,
      settings: save.settings,
      expected_version: save.expectedVersion
    },
    MODULE_SET_TIMEOUT_MS
  );
  const fields = Array.isArray(r.fields) ? r.fields.filter((f): f is string => typeof f === 'string') : [];
  return { version: parseConfigVersion(r.version), code: replyCode(r), error: r.error ?? '', fields };
}

type SetupReply = CodedReply & {
  guild_id?: string;
  live_channel_id?: string;
  clips_channel_id?: string;
  welcome_channel_id?: string;
  voice_hub_id?: string;
  voice_category_id?: string;
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
  dropped_pins?: string[];
};

export type DiscordSetup = {
  config: DiscordConfig;
  refused: string;
  droppedPins: PinnedSlot[];
  error: string;
  code: DiscordCode;
};

export const SETUP_FIELDS: [keyof DiscordConfig, keyof SetupReply][] = [
  ['guildId', 'guild_id'],
  ['liveChannelId', 'live_channel_id'],
  ['clipsChannelId', 'clips_channel_id'],
  ['welcomeChannelId', 'welcome_channel_id'],
  ['voiceHubId', 'voice_hub_id'],
  ['voiceCategoryId', 'voice_category_id'],
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

export const SETUP_REQUEST_KEYS = [
  'user_id',
  'guild_id',
  'subscribers',
  'pinned_roles',
  'installed_by',
  'mode'
] as const;

function setupRequest(target: DiscordGuildTarget): Record<(typeof SETUP_REQUEST_KEYS)[number], unknown> {
  return {
    user_id: target.userId,
    guild_id: target.guildId,
    subscribers: target.subscribers === true,
    pinned_roles: target.pinnedRoles ?? {},
    installed_by: target.installedBy ?? '',
    mode: target.mode ?? 'apply'
  };
}

export async function setupGuild(
  target: DiscordGuildTarget,
  current: DiscordConfig
): Promise<DiscordSetup> {
  const r = await rpcReply<SetupReply>(`${SUB.dingressRpc}.discord.setup`, setupRequest(target), SETUP_TIMEOUT_MS);
  if (r.error) {
    return { config: current, refused: '', droppedPins: [], error: r.error, code: replyCode(r) };
  }
  return {
    config: applySetup(current, target.guildId, r),
    refused: r.refused ?? '',
    droppedPins: droppedPinNotice(r.dropped_pins),
    error: '',
    code: ''
  };
}

export const TEMPLATE_ITEM_KEYS = ['kind', 'name', 'type', 'parent', 'action', 'id'] as const;

export type TemplateItem = {
  kind: 'role' | 'channel';
  name: string;
  type?: number;
  parent?: string;
  action: 'create' | 'adopt' | 'pinned';
  id?: string;
};

type TemplateReply = CodedReply & {
  lived_in?: boolean;
  items?: Partial<Record<(typeof TEMPLATE_ITEM_KEYS)[number], unknown>>[];
  dropped_pins?: string[];
};

export type TemplatePlan = {
  livedIn: boolean;
  items: TemplateItem[];
  droppedPins: PinnedSlot[];
  error: string;
  code: DiscordCode;
};

const TEMPLATE_ACTIONS: readonly string[] = ['create', 'adopt', 'pinned'];

type RawTemplateItem = NonNullable<TemplateReply['items']>[number];

function templateCore(raw: RawTemplateItem): TemplateItem | null {
  if (typeof raw.name !== 'string' || raw.name === '') return null;
  if (raw.kind !== 'role' && raw.kind !== 'channel') return null;
  if (typeof raw.action !== 'string' || !TEMPLATE_ACTIONS.includes(raw.action)) return null;
  return { kind: raw.kind, name: raw.name, action: raw.action as TemplateItem['action'] };
}

function templateItem(raw: RawTemplateItem): TemplateItem | null {
  const out = templateCore(raw);
  if (!out) return null;
  if (typeof raw.type === 'number') out.type = raw.type;
  if (typeof raw.parent === 'string' && raw.parent) out.parent = raw.parent;
  if (typeof raw.id === 'string' && raw.id) out.id = raw.id;
  return out;
}

export async function previewTemplate(target: DiscordGuildTarget): Promise<TemplatePlan> {
  const request = setupRequest(target);
  const r = await rpcReply<TemplateReply>(
    `${SUB.dingressRpc}.discord.template.preview`,
    {
      user_id: request.user_id,
      guild_id: request.guild_id,
      subscribers: request.subscribers,
      pinned_roles: request.pinned_roles
    },
    LAYOUT_TIMEOUT_MS
  );
  const items = (r.items ?? []).map(templateItem).filter((i): i is TemplateItem => i !== null);
  return {
    livedIn: r.lived_in === true,
    items,
    droppedPins: droppedPinNotice(r.dropped_pins),
    error: r.error ?? '',
    code: replyCode(r)
  };
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

export async function persistSetup(
  target: DiscordGuildTarget,
  config: DiscordConfig
): Promise<DiscordSaveResult> {
  const fresh = await readGuildConfig(target);
  return saveGuildConfig({ userId: target.userId, guildId: target.guildId, config, expectedVersion: fresh.version });
}

type WireEntry = {
  id?: string;
  name?: string;
  type?: number;
  parent_name?: string;
  bot_can_send?: boolean;
  bot_can_embed?: boolean;
};
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
    .map(entry);
}

function entry(e: WireEntry): DiscordEntry {
  const out: DiscordEntry = { id: String(e.id), name: e.name ?? '', type: e.type ?? 0 };
  if (e.parent_name) out.parentName = e.parent_name;
  if (typeof e.bot_can_send === 'boolean') out.botCanSend = e.bot_can_send;
  if (typeof e.bot_can_embed === 'boolean') out.botCanEmbed = e.bot_can_embed;
  return out;
}

function guildInfo(g: WireGuild | undefined): DiscordGuildInfo {
  return {
    id: g?.id ?? '',
    name: g?.name ?? '',
    iconUrl: g?.icon_url ?? '',
    memberCount: Number(g?.member_count ?? 0)
  };
}

export async function guildLayout(target: DiscordGuildTarget): Promise<DiscordLayout> {
  const r = await rpcReply<LayoutReply>(
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

export async function botStatus(target: DiscordGuildTarget): Promise<DiscordStatus> {
  const r = await rpcReply<StatusReply>(
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

export async function repostDesk(
  target: DiscordGuildTarget,
  config: DiscordConfig
): Promise<DiscordRepost> {
  const r = await rpcReply<CodedReply & { message_id?: string }>(
    `${SUB.dingressRpc}.discord.desk.repost`,
    {
      user_id: target.userId,
      guild_id: target.guildId,
      channel_id: config.ticketChannelId ?? '',
      panel: ticketPanelPayload(config)
    },
    REPOST_TIMEOUT_MS
  );
  return { messageId: r.message_id ?? '', error: r.error ?? '', code: replyCode(r) };
}

export async function unbindGuild(target: DiscordGuildTarget): Promise<void> {
  const r = await rpcReply<CodedReply>(
    `${SUB.dingressRpc}.discord.unbind`,
    { user_id: target.userId, guild_id: target.guildId },
    UNBIND_TIMEOUT_MS
  );
  if (r.error) throw new Error(r.error);
}

export function pinnedRolesOf(config: DiscordConfig): PinnedRoles {
  return parsePinnedRoles(encodePinnedRoles(parsePinnedRoles(config.pinnedRoles)));
}
