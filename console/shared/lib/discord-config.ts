// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The Discord module blob, its defaults, and every rule that decides whether a
// value the dashboard submits is allowed to reach the row.
//
// This lives in shared/ rather than beside the route for one reason: the
// console test runner only executes `console/shared/**` plus one dashboard
// file (see console/package.json "test"), so a parser left next to the route
// is a parser nobody ever runs. It is also the mirror of the Go side —
// internal/domain/discord/config.go holds the same field names and
// ValidateConfig holds the same rules — and the two drift silently unless the
// console half is pinned by tests of its own.
//
// Every field is a string. The blob is a flat map<string,string> in MySQL that
// both sesame and outgress read; typed values would have to be re-encoded at
// both ends, and a missing key has to be distinguishable from an explicit
// empty one, which is why the tri-state flags below are '' / 'on' / 'off'
// rather than booleans.

/** Discord snowflake: 17-20 digits. Discord's own docs cap ids at 20. */
const SNOWFLAKE = /^\d{17,20}$/;

/** #rrggbb, the only colour shape the embed editor writes. */
const HEX6 = /^#[0-9a-f]{6}$/;

/**
 * LIVE_COLOR_HEX mirrors internal/domain/discord/embed.go:33
 * (`const LiveColor = 0xC47A3A`) — the warm amber every Bagel embed already
 * uses. Duplicated rather than fetched because the dashboard has to render the
 * swatch before any RPC round trip, and a colour that arrives late paints the
 * preview twice.
 */
export const LIVE_COLOR_HEX = '#c47a3a';

/** Ticket panel copy defaults. These are what Discord actually posts when the
 *  field is left empty, so they are shown verbatim as placeholders rather than
 *  translated: a French placeholder would promise a French embed. */
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

/** The role slots a streamer can pin to an existing guild role. Order is the
 *  encoding order, so `encodePinnedRoles` is stable across saves. */
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

/** How each field is checked. One table, so adding a field is one row rather
 *  than a new branch in three functions. */
type FieldKind = 'snowflake' | 'snowflakeList' | 'flag' | 'limit' | 'color' | 'pinned' | 'text';

type Rule = { kind: FieldKind; max?: number };

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

/** Every field defaults to the empty string: unset, so the Go accessors decide
 *  what "unset" means per field rather than the console guessing. */
export function blankDiscordConfig(): DiscordConfig {
  const out = {} as DiscordConfig;
  for (const key of DISCORD_CONFIG_KEYS) out[key] = '';
  return out;
}

export function isSnowflake(v: string): boolean {
  return SNOWFLAKE.test(v);
}

/** A flag that is ON unless it was explicitly turned off. */
export function alertOn(v: string | undefined): boolean {
  return v !== 'off';
}

/** A flag that is OFF unless it was explicitly turned on. */
export function alertOff(v: string | undefined): boolean {
  return v === 'on';
}

export function flagValue(on: boolean): string {
  return on ? 'on' : 'off';
}

// ── pinned roles ──────────────────────────────────────────────────────────

export type PinnedRoles = Partial<Record<PinnedSlot, string>>;

const SLOT_SET: ReadonlySet<string> = new Set(PINNED_SLOTS);

/** `owner=123,vip=456` → `{owner:'123', vip:'456'}`. Unknown slots and
 *  malformed pairs are dropped rather than throwing: this parses a value that
 *  may predate the current slot list. */
export function parsePinnedRoles(raw: string): PinnedRoles {
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

/** Encodes in PINNED_SLOTS order so a save that changes nothing produces the
 *  same string and does not look like a change to the dirty guard. */
export function encodePinnedRoles(pins: PinnedRoles): string {
  const parts: string[] = [];
  for (const slot of PINNED_SLOTS) {
    const id = (pins[slot] ?? '').trim();
    if (SNOWFLAKE.test(id)) parts.push(`${slot}=${id}`);
  }
  return parts.join(',');
}

export function pinnedRole(config: DiscordConfig, slot: PinnedSlot): string {
  return parsePinnedRoles(config.pinnedRoles)[slot] ?? '';
}

// ── snowflake lists ───────────────────────────────────────────────────────

export function parseIdList(raw: string): string[] {
  const out: string[] = [];
  for (const part of raw.split(',')) {
    const v = part.trim();
    if (SNOWFLAKE.test(v) && !out.includes(v)) out.push(v);
  }
  return out;
}

export function encodeIdList(ids: readonly string[]): string {
  return parseIdList(ids.join(',')).join(',');
}

// ── name lists (stream categories, link allow list) ───────────────────────

/** One category name is at most this long. Twitch's own category names top out
 *  well under it; the cap exists so a paste of a whole sentence becomes a
 *  visible refusal rather than a chip nobody can read. */
export const CATEGORY_NAME_MAX = 40;

export function parseNameList(raw: string): string[] {
  const out: string[] = [];
  for (const part of raw.split(',')) {
    const v = part.trim();
    if (v === '' || v.length > CATEGORY_NAME_MAX) continue;
    if (!out.some((n) => n.toLowerCase() === v.toLowerCase())) out.push(v);
  }
  return out;
}

export function encodeNameList(names: readonly string[]): string {
  return parseNameList(names.join(',')).join(', ');
}

// ── colour ────────────────────────────────────────────────────────────────

/** Accepts `#rgb`, `#rrggbb` and the same two without the hash, in any case.
 *  Anything else falls back rather than painting an embed a colour the
 *  streamer did not choose. */
export function normalizeHex(raw: string, fallback = LIVE_COLOR_HEX): string {
  const v = raw.trim().toLowerCase().replace(/^#/, '');
  if (/^[0-9a-f]{3}$/.test(v)) return `#${v[0]}${v[0]}${v[1]}${v[1]}${v[2]}${v[2]}`;
  if (/^[0-9a-f]{6}$/.test(v)) return `#${v}`;
  return fallback;
}

export function isHexColor(raw: string): boolean {
  return HEX6.test(raw.trim().toLowerCase());
}

// ── ticket panel ──────────────────────────────────────────────────────────

export type TicketPanelSpec = { title: string; body: string; button: string; color: string };

export function ticketPanelSpec(config: DiscordConfig): TicketPanelSpec {
  return {
    title: config.ticketPanelTitle || TICKET_PANEL_DEFAULTS.title,
    body: config.ticketPanelBody || TICKET_PANEL_DEFAULTS.body,
    button: config.ticketPanelButton || TICKET_PANEL_DEFAULTS.button,
    color: normalizeHex(config.ticketPanelColor, TICKET_PANEL_DEFAULTS.color)
  };
}

export function ticketOpenLimitN(config: DiscordConfig): number {
  const n = Number.parseInt(config.ticketOpenLimit, 10);
  if (!Number.isFinite(n)) return TICKET_OPEN_LIMIT_DEFAULT;
  if (n < TICKET_OPEN_LIMIT_MIN) return TICKET_OPEN_LIMIT_DEFAULT;
  if (n > TICKET_OPEN_LIMIT_MAX) return TICKET_OPEN_LIMIT_DEFAULT;
  return n;
}

/** Staff who see and claim tickets: the explicit list when set, otherwise the
 *  three staff role slots — the same fallback the Go accessor uses. */
export function ticketStaffRoleIds(config: DiscordConfig): string[] {
  const explicit = parseIdList(config.ticketStaffRoleIds);
  if (explicit.length) return explicit;
  return parseIdList([config.ownerRoleId, config.leadModRoleId, config.modsRoleId].join(','));
}

export function ticketLogChannel(config: DiscordConfig): string {
  return config.ticketLogChannelId || config.logChannelId;
}

// ── validation + merge ────────────────────────────────────────────────────

export type FieldError = { field: keyof DiscordConfig; code: 'snowflake' | 'list' | 'flag' | 'range' | 'color' | 'pinned' | 'length' };

const CHECKS: Record<FieldKind, (v: string, max: number) => boolean> = {
  snowflake: (v) => SNOWFLAKE.test(v),
  snowflakeList: (v) => v.split(',').every((p) => SNOWFLAKE.test(p.trim())),
  flag: (v) => v === 'on' || v === 'off',
  limit: (v) => integerInRange(v, TICKET_OPEN_LIMIT_MIN, TICKET_OPEN_LIMIT_MAX),
  color: (v) => isHexColor(v),
  pinned: (v) => v.split(',').every(isPinnedPair),
  text: (v, max) => v.length <= max
};

const CODES: Record<FieldKind, FieldError['code']> = {
  snowflake: 'snowflake',
  snowflakeList: 'list',
  flag: 'flag',
  limit: 'range',
  color: 'color',
  pinned: 'pinned',
  text: 'length'
};

function integerInRange(v: string, min: number, max: number): boolean {
  if (!/^\d+$/.test(v)) return false;
  const n = Number.parseInt(v, 10);
  return n >= min && n <= max;
}

function isPinnedPair(part: string): boolean {
  const [slot, id] = part.split('=');
  return SLOT_SET.has((slot ?? '').trim()) && SNOWFLAKE.test((id ?? '').trim());
}

/** Empty always passes: it is the "unset" value for every field, and the Go
 *  accessors decide the default. */
function accepts(rule: Rule, value: string): boolean {
  if (value === '') return true;
  return CHECKS[rule.kind](value, rule.max ?? Number.MAX_SAFE_INTEGER);
}

export function validateDiscordConfig(config: DiscordConfig): FieldError[] {
  const out: FieldError[] = [];
  for (const key of DISCORD_CONFIG_KEYS) {
    const rule = FIELD_RULES[key];
    if (accepts(rule, config[key])) continue;
    out.push({ field: key, code: CODES[rule.kind] });
  }
  return out;
}

/** Reads a stored blob back. Non-string values and unknown keys are dropped:
 *  the row is written by this console but read by two Go services, and a
 *  number that sneaks in would break their string decode. */
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

/**
 * Applies a submitted draft on top of the stored blob.
 *
 * A field the draft does not carry keeps its stored value, and a field that
 * fails its rule keeps its stored value *and* reports an error — never a
 * silent blank. The page has already validated the same rules client-side, so
 * an error here means a stale form, a hand-rolled POST, or drift between the
 * two halves; blanking a channel id in any of those cases silently stops
 * Bagel posting, which is the worst possible failure for this page.
 */
export function mergeDiscordConfig(current: DiscordConfig, patch: Record<string, unknown>): MergeResult {
  const config = { ...current };
  const errors: FieldError[] = [];
  for (const key of DISCORD_CONFIG_KEYS) {
    const raw = patch[key];
    if (typeof raw !== 'string') continue;
    const value = normalizeField(FIELD_RULES[key], raw);
    if (accepts(FIELD_RULES[key], value)) config[key] = value;
    else errors.push({ field: key, code: CODES[FIELD_RULES[key].kind] });
  }
  return { config, errors };
}

/** Trims, and re-encodes the two list shapes so the stored string is canonical
 *  whatever spacing the form sent. */
function normalizeField(rule: Rule, raw: string): string {
  const v = raw.trim();
  if (rule.kind === 'snowflakeList') return encodeIdList(v.split(','));
  if (rule.kind === 'pinned') return encodePinnedRoles(parsePinnedRoles(v));
  if (rule.kind === 'color' && v !== '') return normalizeHex(v);
  return v;
}
