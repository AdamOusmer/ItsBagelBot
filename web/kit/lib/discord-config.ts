// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type Snowflake = string;
export type FieldText = string;
export type FieldName = string;
export type HexColor = string;
export type GuildName = string;

const SNOWFLAKE = /^\d{17,20}$/;

const HEX6 = /^#[0-9a-f]{6}$/;

export const LIVE_COLOR_HEX = '#c47a3a';

export const TICKET_PANEL_DEFAULTS = {
  title: 'Need help?',
  body: 'Open a private ticket with the staff.',
  button: 'Open a ticket',
  color: LIVE_COLOR_HEX
} as const;

export const TICKET_PANEL_TITLE_MAX = 100;
export const TICKET_PANEL_BODY_MAX = 1000;
export const TICKET_PANEL_BUTTON_MAX = 40;
export const CATEGORY_LIST_MAX = 500;

export const TICKET_OPEN_LIMIT_MIN = 1;
export const TICKET_OPEN_LIMIT_MAX = 5;
export const TICKET_OPEN_LIMIT_DEFAULT = 1;

export const PINNED_SLOTS = [
  'owner',
  'leadMod',
  'mods',
  'vip',
  'subscriber',
  'regulars',
  'member'
] as const;

export type PinnedSlot = (typeof PINNED_SLOTS)[number];

export type DiscordConfig = {
  guildId: string;
  twitchLogin: string;

  liveChannelId: string;
  clipsChannelId: string;
  welcomeChannelId: string;
  voiceHubId: string;
  logChannelId: string;

  subsChannelId: string;
  subsCategoryId: string;
  vipChannelId: string;
  vipCategoryId: string;

  ticketChannelId: string;
  ticketCategoryId: string;
  ticketArchiveCategoryId: string;
  ticketLogChannelId: string;
  ticketStaffRoleIds: string;
  ticketOpenLimit: string;
  ticketTranscriptEnabled: string;
  ticketPanelTitle: string;
  ticketPanelBody: string;
  ticketPanelColor: string;
  ticketPanelButton: string;

  ownerRoleId: string;
  leadModRoleId: string;
  modsRoleId: string;
  vipRoleId: string;
  subscriberRoleId: string;
  regularsRoleId: string;
  memberRoleId: string;
  pinnedRoles: string;

  liveEnabled: string;
  clipsEnabled: string;
  welcomeEnabled: string;
  goodbyeEnabled: string;
  voiceEnabled: string;
  ticketsEnabled: string;
  logsEnabled: string;
  subscribersEnabled: string;
  levelsEnabled: string;
  linkGuardEnabled: string;
  autoRoleEnabled: string;

  categoryAllow: string;
  categoryDeny: string;
  linkAllowList: string;
};

type FieldKind = 'snowflake' | 'snowflakeList' | 'flag' | 'limit' | 'color' | 'pinned' | 'text';

type Rule = { kind: FieldKind; max?: number };

type RuledValue = { rule: Rule; value: FieldText };

type IntRange = { min: number; max: number };

const SNOW: Rule = { kind: 'snowflake' };
const FLAG: Rule = { kind: 'flag' };

export const FIELD_RULES: Record<keyof DiscordConfig, Rule> = {
  guildId: SNOW,
  twitchLogin: { kind: 'text', max: 40 },

  liveChannelId: SNOW,
  clipsChannelId: SNOW,
  welcomeChannelId: SNOW,
  voiceHubId: SNOW,
  logChannelId: SNOW,

  subsChannelId: SNOW,
  subsCategoryId: SNOW,
  vipChannelId: SNOW,
  vipCategoryId: SNOW,

  ticketChannelId: SNOW,
  ticketCategoryId: SNOW,
  ticketArchiveCategoryId: SNOW,
  ticketLogChannelId: SNOW,
  ticketStaffRoleIds: { kind: 'snowflakeList' },
  ticketOpenLimit: { kind: 'limit' },
  ticketTranscriptEnabled: FLAG,
  ticketPanelTitle: { kind: 'text', max: TICKET_PANEL_TITLE_MAX },
  ticketPanelBody: { kind: 'text', max: TICKET_PANEL_BODY_MAX },
  ticketPanelColor: { kind: 'color' },
  ticketPanelButton: { kind: 'text', max: TICKET_PANEL_BUTTON_MAX },

  ownerRoleId: SNOW,
  leadModRoleId: SNOW,
  modsRoleId: SNOW,
  vipRoleId: SNOW,
  subscriberRoleId: SNOW,
  regularsRoleId: SNOW,
  memberRoleId: SNOW,
  pinnedRoles: { kind: 'pinned' },

  liveEnabled: FLAG,
  clipsEnabled: FLAG,
  welcomeEnabled: FLAG,
  goodbyeEnabled: FLAG,
  voiceEnabled: FLAG,
  ticketsEnabled: FLAG,
  logsEnabled: FLAG,
  subscribersEnabled: FLAG,
  levelsEnabled: FLAG,
  linkGuardEnabled: FLAG,
  autoRoleEnabled: FLAG,

  categoryAllow: { kind: 'text', max: CATEGORY_LIST_MAX },
  categoryDeny: { kind: 'text', max: CATEGORY_LIST_MAX },
  linkAllowList: { kind: 'text', max: CATEGORY_LIST_MAX }
};

export const DISCORD_CONFIG_KEYS = Object.keys(FIELD_RULES) as (keyof DiscordConfig)[];

export function blankDiscordConfig(): DiscordConfig {
  const out = {} as DiscordConfig;
  for (const key of DISCORD_CONFIG_KEYS) out[key] = '';
  return out;
}

export function isSnowflake(v: Snowflake): boolean {
  return SNOWFLAKE.test(v);
}

export function alertOn(v: FieldText | undefined): boolean {
  return v !== 'off';
}

export function alertOff(v: FieldText | undefined): boolean {
  return v === 'on';
}

export function flagValue(on: boolean): FieldText {
  return on ? 'on' : 'off';
}

export type PinnedRoles = Partial<Record<PinnedSlot, Snowflake>>;

const SLOT_SET: ReadonlySet<string> = new Set(PINNED_SLOTS);

export function parsePinnedRoles(raw: FieldText): PinnedRoles {
  const out: PinnedRoles = {};
  for (const part of raw.split(',')) {
    const [slot, id] = part.split('=');
    const key = (slot ?? '').trim();
    const value = (id ?? '').trim();
    if (!SLOT_SET.has(key)) continue;
    if (!SNOWFLAKE.test(value)) continue;
    out[key as PinnedSlot] = value;
  }
  return out;
}

export function encodePinnedRoles(pins: PinnedRoles): FieldText {
  const parts: string[] = [];
  for (const slot of PINNED_SLOTS) {
    const id = (pins[slot] ?? '').trim();
    if (SNOWFLAKE.test(id)) parts.push(`${slot}=${id}`);
  }
  return parts.join(',');
}

export function pinnedRole(config: DiscordConfig, slot: PinnedSlot): Snowflake {
  return parsePinnedRoles(config.pinnedRoles)[slot] ?? '';
}

export function clearPinnedSlots(config: DiscordConfig, slots: readonly PinnedSlot[]): DiscordConfig {
  if (slots.length === 0) return config;
  const pins = parsePinnedRoles(config.pinnedRoles);
  for (const slot of slots) delete pins[slot];
  return { ...config, pinnedRoles: encodePinnedRoles(pins) };
}

export function droppedPinNotice(slots: readonly unknown[] | null | undefined): PinnedSlot[] {
  const seen = new Set<string>();
  for (const raw of slots ?? []) {
    const name = typeof raw === 'string' ? raw.trim() : '';
    if (SLOT_SET.has(name)) seen.add(name);
  }
  return PINNED_SLOTS.filter((slot) => seen.has(slot));
}

export function parseIdList(raw: FieldText): Snowflake[] {
  const out: Snowflake[] = [];
  for (const part of raw.split(',')) {
    const v = part.trim();
    if (SNOWFLAKE.test(v) && !out.includes(v)) out.push(v);
  }
  return out;
}

export function encodeIdList(ids: readonly Snowflake[]): FieldText {
  return parseIdList(ids.join(',')).join(',');
}

export const CATEGORY_NAME_MAX = 40;

export function parseNameList(raw: FieldText): string[] {
  const out: string[] = [];
  for (const part of raw.split(',')) {
    const v = part.trim();
    if (v === '' || v.length > CATEGORY_NAME_MAX) continue;
    if (!out.some((n) => n.toLowerCase() === v.toLowerCase())) out.push(v);
  }
  return out;
}

export function encodeNameList(names: readonly string[]): FieldText {
  return parseNameList(names.join(',')).join(', ');
}

export function normalizeHex(raw: FieldText, fallback: HexColor = LIVE_COLOR_HEX): HexColor {
  const v = raw.trim().toLowerCase().replace(/^#/, '');
  if (/^[0-9a-f]{3}$/.test(v)) return `#${v[0]}${v[0]}${v[1]}${v[1]}${v[2]}${v[2]}`;
  if (/^[0-9a-f]{6}$/.test(v)) return `#${v}`;
  return fallback;
}

export function isHexColor(raw: FieldText): boolean {
  return HEX6.test(raw.trim().toLowerCase());
}

export function hexToDiscordColor(raw: FieldText): number | null {
  const v = raw.trim();
  if (v === '' || !HEX_INPUT.test(v)) return null;
  return Number.parseInt(normalizeHex(v).slice(1), 16);
}

export type TicketPanelSpec = { title: string; body: string; button: string; color: HexColor };

export function ticketPanelSpec(config: DiscordConfig): TicketPanelSpec {
  return {
    title: config.ticketPanelTitle || TICKET_PANEL_DEFAULTS.title,
    body: config.ticketPanelBody || TICKET_PANEL_DEFAULTS.body,
    button: config.ticketPanelButton || TICKET_PANEL_DEFAULTS.button,
    color: normalizeHex(config.ticketPanelColor, TICKET_PANEL_DEFAULTS.color)
  };
}

export type TicketPanelPayload = { title: string; body: string; button: string; color?: number };

export function ticketPanelPayload(config: DiscordConfig): TicketPanelPayload {
  const spec = ticketPanelSpec(config);
  const color = hexToDiscordColor(config.ticketPanelColor);
  return {
    title: spec.title,
    body: spec.body,
    button: spec.button,
    ...(color === null ? {} : { color })
  };
}

export function ticketOpenLimitN(config: DiscordConfig): number {
  const n = Number.parseInt(config.ticketOpenLimit, 10);
  if (!Number.isFinite(n)) return TICKET_OPEN_LIMIT_DEFAULT;
  if (n < TICKET_OPEN_LIMIT_MIN) return TICKET_OPEN_LIMIT_DEFAULT;
  if (n > TICKET_OPEN_LIMIT_MAX) return TICKET_OPEN_LIMIT_DEFAULT;
  return n;
}

export function ticketStaffRoleIds(config: DiscordConfig): Snowflake[] {
  const explicit = parseIdList(config.ticketStaffRoleIds);
  if (explicit.length) return explicit;
  return parseIdList([config.ownerRoleId, config.leadModRoleId, config.modsRoleId].join(','));
}

export function ticketLogChannel(config: DiscordConfig): Snowflake {
  return config.ticketLogChannelId || config.logChannelId;
}

export type FieldError = { field: keyof DiscordConfig; code: 'snowflake' | 'list' | 'flag' | 'range' | 'color' | 'pinned' | 'length' };

const CHECKS: Record<FieldKind, (field: RuledValue) => boolean> = {
  snowflake: ({ value }) => SNOWFLAKE.test(value),
  snowflakeList: ({ value }) => value.split(',').every((p) => SNOWFLAKE.test(p.trim())),
  flag: ({ value }) => value === 'on' || value === 'off',
  limit: ({ value }) => integerInRange(value, TICKET_OPEN_LIMIT_RANGE),
  color: ({ value }) => HEX_INPUT.test(value.trim()),
  pinned: ({ value }) => value.split(',').every(isPinnedPair),
  text: ({ value, rule }) => value.length <= (rule.max ?? Number.MAX_SAFE_INTEGER)
};

const TICKET_OPEN_LIMIT_RANGE: IntRange = {
  min: TICKET_OPEN_LIMIT_MIN,
  max: TICKET_OPEN_LIMIT_MAX
};

const HEX_INPUT = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i;

const CODES: Record<FieldKind, FieldError['code']> = {
  snowflake: 'snowflake',
  snowflakeList: 'list',
  flag: 'flag',
  limit: 'range',
  color: 'color',
  pinned: 'pinned',
  text: 'length'
};

function integerInRange(v: FieldText, range: IntRange): boolean {
  if (!/^\d+$/.test(v)) return false;
  const n = Number.parseInt(v, 10);
  return n >= range.min && n <= range.max;
}

function isPinnedPair(part: FieldText): boolean {
  const [slot, id] = part.split('=');
  return SLOT_SET.has((slot ?? '').trim()) && SNOWFLAKE.test((id ?? '').trim());
}

function accepts(field: RuledValue): boolean {
  if (field.value === '') return true;
  return CHECKS[field.rule.kind](field);
}

export type RefusedFields = {
  byField: Partial<Record<keyof DiscordConfig, true>>;
  first: keyof DiscordConfig | '';
  unknown: string[];
};

export function fieldErrorsByField(fields: readonly unknown[] | null | undefined): RefusedFields {
  const out: RefusedFields = { byField: {}, first: '', unknown: [] };
  for (const raw of fields ?? []) {
    const name = typeof raw === 'string' ? raw.trim() : '';
    if (!FIELD_SET.has(name)) {
      rememberUnknown(out, name);
      continue;
    }
    const key = name as keyof DiscordConfig;
    out.byField[key] = true;
    if (out.first === '') out.first = key;
  }
  return out;
}

const FIELD_SET: ReadonlySet<string> = new Set<string>(DISCORD_CONFIG_KEYS);

function rememberUnknown(out: RefusedFields, name: FieldName): void {
  if (name === '') return;
  if (out.unknown.includes(name)) return;
  out.unknown.push(name);
}

export function parseDiscordConfig(raw: unknown): DiscordConfig {
  const out = blankDiscordConfig();
  if (raw === null) return out;
  if (typeof raw !== 'object') return out;
  if (Array.isArray(raw)) return out;
  const src = raw as Record<string, unknown>;
  for (const key of DISCORD_CONFIG_KEYS) {
    const v = src[key];
    if (typeof v === 'string') out[key] = v;
  }
  return out;
}

export type MergeResult = { config: DiscordConfig; errors: FieldError[] };

export function mergeDiscordConfig(current: DiscordConfig, patch: Record<string, unknown>): MergeResult {
  const config = { ...current };
  const errors: FieldError[] = [];
  for (const key of DISCORD_CONFIG_KEYS) {
    const raw = patch[key];
    if (typeof raw !== 'string') continue;
    const field: RuledValue = { rule: FIELD_RULES[key], value: raw.trim() };
    if (!accepts(field)) {
      errors.push({ field: key, code: CODES[field.rule.kind] });
      continue;
    }
    config[key] = normalizeField(field);
  }
  return { config, errors };
}

function normalizeField(field: RuledValue): FieldText {
  const { rule, value } = field;
  if (rule.kind === 'snowflakeList') return encodeIdList(value.split(','));
  if (rule.kind === 'pinned') return encodePinnedRoles(parsePinnedRoles(value));
  if (rule.kind === 'color' && value !== '') return normalizeHex(value);
  return value;
}

/** 0, not -1: outgress's stored version starts at 0, so -1 makes the first save a conflict. */
export const DISCORD_CONFIG_VERSION_NEW = 0;

export function parseConfigVersion(raw: unknown): number {
  if (isUsableVersionNumber(raw)) return raw;
  if (isDigitString(raw)) {
    const n = Number.parseInt(raw.trim(), 10);
    if (Number.isSafeInteger(n)) return n;
  }
  return DISCORD_CONFIG_VERSION_NEW;
}

function isUsableVersionNumber(raw: unknown): raw is number {
  if (typeof raw !== 'number') return false;
  if (!Number.isSafeInteger(raw)) return false;
  return raw >= 0;
}

function isDigitString(raw: unknown): raw is string {
  if (typeof raw !== 'string') return false;
  return /^\d+$/.test(raw.trim());
}

export const DISCORD_ADMINISTRATOR = 0x8n;
export const DISCORD_MANAGE_GUILD = 0x20n;

export type GuildPermissionEntry = {
  id?: string;
  name?: string;
  owner?: boolean;
  permissions?: string | number;
};

export function guildPermissionBits(raw: GuildPermissionEntry['permissions']): bigint {
  if (typeof raw === 'number') {
    if (!Number.isSafeInteger(raw) || raw < 0) return 0n;
    return BigInt(raw);
  }
  const v = (raw ?? '').trim();
  if (!/^\d+$/.test(v)) return 0n;
  return BigInt(v);
}

export function canManageGuild(entry: GuildPermissionEntry): boolean {
  if (entry.owner === true) return true;
  const bits = guildPermissionBits(entry.permissions);
  return (bits & DISCORD_ADMINISTRATOR) !== 0n || (bits & DISCORD_MANAGE_GUILD) !== 0n;
}

export function guildMonogram(name: GuildName): string {
  const words = name.trim().split(/\s+/).filter((w) => w !== '');
  if (words.length === 0) return '?';
  if (words.length === 1) return [...words[0]].slice(0, 2).join('').toUpperCase();
  return (([...words[0]][0] ?? '') + ([...words[1]][0] ?? '')).toUpperCase();
}

const DISCORD_ICON_CDN = 'https://cdn.discordapp.com/icons/';

export function guildIconURL(guildId: Snowflake, icon: string): string {
  if (!isSnowflake(guildId) || !/^[a-z0-9_]+$/i.test(icon)) return '';
  return `${DISCORD_ICON_CDN}${guildId}/${icon}.png`;
}

export function guildIconSrc(url: string, size = 128): string {
  if (!url.startsWith(DISCORD_ICON_CDN)) return url;
  return `${url}?size=${size}`;
}

export type GuildBotState = 'online' | 'offline' | 'reauth' | 'unknown';

export function guildBotState(g: {
  botPresent?: boolean;
  needsReauth?: boolean;
  reauthUnknown?: boolean;
}): GuildBotState {
  if (g.needsReauth === true) return 'reauth';
  if (g.reauthUnknown === true) return 'unknown';
  return g.botPresent === true ? 'online' : 'offline';
}

export type GuildPickerBadge = 'mine' | 'elsewhere' | 'addable';

export type GuildPickerLists = {
  bound: readonly Snowflake[];
  elsewhere?: readonly Snowflake[];
};

export function guildPickerBadge(guildId: Snowflake, lists: GuildPickerLists): GuildPickerBadge {
  if (lists.bound.includes(guildId)) return 'mine';
  if ((lists.elsewhere ?? []).includes(guildId)) return 'elsewhere';
  return 'addable';
}

export type DiscordUserGuild = {
  id: Snowflake;
  name: GuildName;
  icon: string;
  owner: boolean;
  permissions: string;
};

export function parseUserGuild(raw: unknown): DiscordUserGuild | null {
  if (raw === null || typeof raw !== 'object') return null;
  if (Array.isArray(raw)) return null;
  const g = raw as { id?: unknown; name?: unknown; icon?: unknown; owner?: unknown; permissions?: unknown };
  if (typeof g.id !== 'string' || g.id === '') return null;
  return {
    id: g.id,
    name: wireString(g.name),
    icon: wireString(g.icon),
    owner: g.owner === true,
    permissions: wireString(g.permissions)
  };
}

function wireString(v: unknown): string {
  return typeof v === 'string' ? v : '';
}

export function parseUserGuilds(raw: unknown): DiscordUserGuild[] | null {
  if (!Array.isArray(raw)) return null;
  const out: DiscordUserGuild[] = [];
  for (const entry of raw) {
    const g = parseUserGuild(entry);
    if (g) out.push(g);
  }
  return out;
}

export function legacyConfigFor(blob: unknown, guildId: Snowflake): DiscordConfig | null {
  if (!isSnowflake(guildId)) return null;
  const parsed = parseDiscordConfig(blob);
  if (parsed.guildId !== guildId) return null;
  if (!carriesLegacyFields(parsed)) return null;
  return parsed;
}

function carriesLegacyFields(config: DiscordConfig): boolean {
  return DISCORD_CONFIG_KEYS.some(
    (key) => key !== 'guildId' && key !== 'twitchLogin' && config[key] !== ''
  );
}
