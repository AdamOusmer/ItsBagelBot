// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Discord module row + outgress RPCs.
//
// One broadcaster owns MANY guilds. The per-user module row therefore holds
// only the master switch and the Twitch login; every channel, role and toggle
// lives in a per-guild row that outgress owns and serves through
// `config.get` / `config.set`. Before this, the whole config sat in the
// modules blob, which structurally allowed exactly one server per broadcaster
// and had no version to guard a concurrent save with.
//
// The config's shape, defaults and validation live in @bagel/kit's
// discord-config so they can be unit-tested (the console runner only executes
// shared/**); this file is the transport half.
import { rpc } from '@bagel/kit/server/nats';
import { codeReader, type CodedReply } from '@bagel/kit/server/rpc-code';
import {
  MOD,
  droppedPinNotice,
  encodePinnedRoles,
  parseConfigVersion,
  parseDiscordConfig,
  parsePinnedRoles,
  ticketPanelPayload,
  type DiscordConfig,
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
// A per-guild config row read: one Valkey hit or one MySQL row through
// discord-data. The shared read default (2 s) is right for it.
const CONFIG_GET_TIMEOUT_MS = 2000;
const CONFIG_SET_TIMEOUT_MS = 5000;
// guilds.list is one binding lookup plus a GetGuildWithCounts per bound guild.
// Discord's own calls are cached by outgress, but a cold list of half a dozen
// servers still walks them, so this sits well above the write default rather
// than turning a slow-but-working list into a degraded page.
const GUILDS_TIMEOUT_MS = 8000;

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
// Integration fix (2026-09-05): timeout, not_found and unknown were missing.
// An unrecognised code falls through to messageCode, which returns '' -- so a
// guilds.list that timed out part-way, or outgress's explicit "I do not know
// what this error was", both reached the page as code:'' and read as success.
// CodeUnknown exists precisely to stop that, and dropping it here undid it.
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

// The reader (code first, the pre-code sentence as a dying fallback) is
// @bagel/kit/server/rpc-code, shared with every other surface that reads a
// refusal. This page keeps only its own vocabulary: three of these codes are
// discord-specific and the shared set deliberately does not carry them.
export const replyCode: (r: CodedReply) => DiscordCode = codeReader(DISCORD_CODES);

/**
 * A refused guild RPC, carrying the code the reply named.
 *
 * The throwing readers used to raise a plain `Error(r.error)`, which threw the
 * code away and left every caller with an English sentence it could only
 * re-render as the generic failure. That is how a `not_bound` refusal on a
 * first install reached the streamer as "Discord did not answer" about a
 * server whose bot had just been added. Callers that do not branch on the
 * refusal are unaffected: this is still an Error.
 */
export class DiscordRefusal extends Error {
  readonly code: DiscordCode;

  constructor(code: DiscordCode, message: string) {
    super(message);
    this.name = 'DiscordRefusal';
    this.code = code;
  }
}

/** The refusal code behind a thrown error, or '' for anything else (a
 *  transport failure, a bug) -- neither of which is a named refusal. */
export function refusalCode(err: unknown): DiscordCode {
  return err instanceof DiscordRefusal ? err.code : '';
}

// The module row: the master switch and the Twitch login, plus every guild
// this broadcaster has bound. No channel or role ids: those are per guild.
export type DiscordView = {
  enabled: boolean;
  twitchLogin: string;
  guilds: DiscordGuildSummary[];
  // True when outgress had more bindings than it listed (its own cap). It is
  // a property of the LIST, not of any guild in it: a streamer with more
  // servers than the cap otherwise sees a short list and no sign of it, which
  // reads as Bagel having lost a server rather than as a page showing the
  // first N.
  truncated: boolean;
};

// One row of the server list. needsReauth is per guild, not per account:
// outgress learns it from Discord's own 403 on a rename, and a broadcaster
// with four servers can have three healthy and one whose install predates the
// permission. Integration fix (2026-09-05): this used to be read
// optimistically against a field guilds.list did not send, so the reauth pill
// was unreachable; DiscordGuildEntry.NeedsReauth now carries it.
export type DiscordGuildSummary = {
  guildId: string;
  name: string;
  iconUrl: string;
  memberCount: number;
  botPresent: boolean;
  needsReauth: boolean;
  // reauthUnknown says needsReauth was never actually read for this guild, so
  // false above means "we do not know", not "the grant is fine". The listing's
  // reauth lookups run after its deadline can pass, and every one of them then
  // reports false -- a dead grant rendered as a healthy server on exactly the
  // slow load where the streamer is already suspicious. The pill goes neutral
  // on this rather than green (see guildBotState).
  reauthUnknown: boolean;
  boundAtMs: number;
};

// A per-guild config row. `found` false is a guild that is bound but has never
// been saved: the page renders defaults and the first save writes version 1.
export type DiscordGuildConfig = {
  config: DiscordConfig;
  version: number;
  found: boolean;
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
  // installedBy is who actually pressed the button, which is not userId when
  // a staff member is impersonating a broadcaster. It is recorded on the
  // binding for support, and nothing branches on it.
  installedBy?: string;
};

export type DiscordSave = { userId: string; enabled: boolean; twitchLogin: string };

export async function readDiscord(user: DiscordUser): Promise<DiscordView> {
  const { enabled, configs } = await readModuleBlob<unknown>(user.userId, DISCORD_MODULE);
  const list = await listGuildsPage(user);
  return {
    enabled,
    // Still parsed through the full config parser: the row predates the split
    // and a board that has not been touched since still carries the old blob,
    // whose extra keys we now simply ignore.
    twitchLogin: parseDiscordConfig(configs).twitchLogin,
    guilds: list.guilds,
    truncated: list.truncated
  };
}

/**
 * The per-user blob as it stands right now, before any narrowing.
 *
 * Read lazily rather than folded into `readDiscord`: it is only interesting on
 * the one path that migrates a pre-split board (see `legacyConfigFor`), and
 * putting it on `DiscordView` would ship the whole old config down to every
 * page render for nothing.
 */
export async function readLegacyBlob(user: DiscordUser): Promise<DiscordConfig> {
  const { configs } = await readModuleBlob<unknown>(user.userId, DISCORD_MODULE);
  return parseDiscordConfig(configs);
}

/**
 * Writes the module row back.
 *
 * Only two fields go in. The old blob's channel and role ids are deliberately
 * NOT carried forward: the first save after this ships narrows the row, and
 * anything still reading a snowflake out of `MOD.discord` is reading a value
 * that is no longer maintained. The per-guild rows are the source of truth.
 *
 * Callers must have written those ids onto the guild row FIRST: narrowing is
 * destructive and there is no second copy. `legacyConfigFor` names the blob
 * that still needs migrating.
 */
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

// listGuildsPage is the whole reply: the rows plus whether outgress had more
// of them than it sent. Every caller that only needs the rows goes through
// listGuilds below, so the flag cannot be dropped on the floor by accident in
// the two places that do an ownership check with it.
export async function listGuildsPage(user: DiscordUser): Promise<DiscordGuildList> {
  const r = await rpc<GuildsReply>(
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

// listGuilds is the ownership check as well as the list: a guild absent from
// this reply is one this broadcaster does not own, and every per-guild route
// 404s on that rather than trusting the id in the URL.
export async function listGuilds(user: DiscordUser): Promise<DiscordGuildSummary[]> {
  return (await listGuildsPage(user)).guilds;
}

type ConfigGetReply = CodedReply & { config?: unknown; version?: unknown; found?: boolean };

export async function readGuildConfig(target: DiscordGuildTarget): Promise<DiscordGuildConfig> {
  const r = await rpc<ConfigGetReply>(
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

/**
 * Writes one guild's config, refusing if the row moved since it was read.
 *
 * expected_version is not optimism for its own sake: a broadcaster's mods can
 * hold this page open on two screens, and the module row it replaced merged
 * blindly, so the second save silently reverted the first one's channel
 * pickers. A `conflict` reply is surfaced as "someone else saved, reload"
 * rather than retried, because the two drafts differ in ways only a human can
 * reconcile.
 */
export async function saveGuildConfig(save: DiscordGuildSave): Promise<DiscordSaveResult> {
  const r = await rpc<CodedReply & { version?: unknown }>(
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
  // The pins outgress could not honour because the role is gone from the
  // guild. It created a replacement for each, so the streamer's pick has
  // silently changed and the page has to say so.
  dropped_pins?: string[];
};

export type DiscordSetup = {
  config: DiscordConfig;
  refused: string;
  droppedPins: PinnedSlot[];
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
      pinned_roles: target.pinnedRoles ?? {},
      installed_by: target.installedBy ?? ''
    },
    SETUP_TIMEOUT_MS
  );
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

/**
 * Writes a finished setup's snowflakes into the guild row.
 *
 * The version is re-read immediately before the write rather than carried in
 * from the page's load. Setup writes the same row on the outgress side, so the
 * version the page holds is routinely one behind by the time a 40-second fill
 * returns, and refusing the rebuild the streamer just asked for with "someone
 * else saved this" would be a lie. Safe because the reply carries exactly what
 * outgress wrote, so re-applying it is idempotent.
 */
export async function persistSetup(
  target: DiscordGuildTarget,
  config: DiscordConfig
): Promise<DiscordSaveResult> {
  const fresh = await readGuildConfig(target);
  return saveGuildConfig({ userId: target.userId, guildId: target.guildId, config, expectedVersion: fresh.version });
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
export async function repostDesk(
  target: DiscordGuildTarget,
  config: DiscordConfig
): Promise<DiscordRepost> {
  // The panel and the channel travel with the request. Integration fix
  // (2026-09-05): the call used to send neither, and outgress's RepostDesk
  // fills a missing Panel from TicketPanelSpec.OrDefaults() rather than from
  // the guild row -- so pressing "Repost panel" right after saving a custom
  // title, body, colour and button posted the stock English panel instead,
  // and on a desk that had never been posted the missing channel failed the
  // call outright. Sending both makes the reposted panel exactly what the
  // editor above it shows.
  // The payload OMITS `color` unless the streamer picked one:
  // DiscordPanelSpec.Color is a *int where absent means "brand default" and 0
  // means #000000. Sending the fallback amber for an untouched panel froze
  // today's brand colour into the wire, and sending 0 for "unset" made black
  // unsavable. ticketPanelPayload owns that decision so it can be tested.
  const r = await rpc<CodedReply & { message_id?: string }>(
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
