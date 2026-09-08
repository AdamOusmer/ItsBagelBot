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

/**
 * The kinds of value this file moves around.
 *
 * All of them are strings on the wire -- the blob is a flat
 * map<string,string> that two Go services read (see the header) -- and that
 * is exactly what these names are for: a snowflake, a stored field value, a
 * field NAME and a hex colour were all spelled `string` in every signature,
 * so no signature said which one it wanted and passing a field name where a
 * field value was meant type-checked. They are aliases rather than branded or
 * wrapped types on purpose: the wire shape must not change, and every
 * existing caller holding a plain string has to keep compiling.
 */
export type Snowflake = string;
/** One config field's stored or submitted value. */
export type FieldText = string;
/** A DiscordConfig key as it arrives from outgress: not yet known to be one. */
export type FieldName = string;
/** `#rrggbb`, the canonical colour shape (see normalizeHex). */
export type HexColor = string;
/** A Discord guild's display name. */
export type GuildName = string;

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

/**
 * One field's value together with the rule it has to satisfy.
 *
 * The two were a `(rule, value)` argument pair through every check, the
 * validator and the merge, and the one check that needs the length cap took a
 * third loose `max` that each caller unpacked from the rule first -- so a
 * check got either its own cap or Number.MAX_SAFE_INTEGER depending on who
 * called it. Kept together, the rule travels with the value it governs and a
 * check reads whatever part of it that check actually needs.
 */
type RuledValue = { rule: Rule; value: FieldText };

/** The inclusive bounds a numeric field is checked against. */
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

/** Every field defaults to the empty string: unset, so the Go accessors decide
 *  what "unset" means per field rather than the console guessing. */
export function blankDiscordConfig(): DiscordConfig {
  const out = {} as DiscordConfig;
  for (const key of DISCORD_CONFIG_KEYS) out[key] = '';
  return out;
}

export function isSnowflake(v: Snowflake): boolean {
  return SNOWFLAKE.test(v);
}

/** A flag that is ON unless it was explicitly turned off. */
export function alertOn(v: FieldText | undefined): boolean {
  return v !== 'off';
}

/** A flag that is OFF unless it was explicitly turned on. */
export function alertOff(v: FieldText | undefined): boolean {
  return v === 'on';
}

export function flagValue(on: boolean): FieldText {
  return on ? 'on' : 'off';
}

// ── pinned roles ──────────────────────────────────────────────────────────

export type PinnedRoles = Partial<Record<PinnedSlot, Snowflake>>;

const SLOT_SET: ReadonlySet<string> = new Set(PINNED_SLOTS);

/** `owner=123,vip=456` → `{owner:'123', vip:'456'}`. Unknown slots and
 *  malformed pairs are dropped rather than throwing: this parses a value that
 *  may predate the current slot list. */
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

/** Encodes in PINNED_SLOTS order so a save that changes nothing produces the
 *  same string and does not look like a change to the dirty guard. */
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

/**
 * Drops slots from the pin list, keeping every other pin untouched.
 *
 * Setup reports the pins whose role no longer exists in the guild; leaving
 * those in the blob would make the NEXT setup try to adopt the same dead id
 * again, and the picker would go on showing "Pinned" over a role Discord has
 * already forgotten. Clearing them is what makes the freshly created role the
 * one the page displays.
 */
export function clearPinnedSlots(config: DiscordConfig, slots: readonly PinnedSlot[]): DiscordConfig {
  if (slots.length === 0) return config;
  const pins = parsePinnedRoles(config.pinnedRoles);
  for (const slot of slots) delete pins[slot];
  return { ...config, pinnedRoles: encodePinnedRoles(pins) };
}

/**
 * Normalises the `dropped_pins` list a setup reply carries.
 *
 * The wire list is whatever outgress found stale, so it can repeat a slot, can
 * name a slot this console version does not have yet, and arrives in the order
 * the fill happened to walk the guild in. The banner reads better in a fixed
 * order, and an unknown slot has no label to print, so both are handled here
 * rather than in the page.
 */
export function droppedPinNotice(slots: readonly unknown[] | null | undefined): PinnedSlot[] {
  const seen = new Set<string>();
  for (const raw of slots ?? []) {
    const name = typeof raw === 'string' ? raw.trim() : '';
    if (SLOT_SET.has(name)) seen.add(name);
  }
  return PINNED_SLOTS.filter((slot) => seen.has(slot));
}

// ── snowflake lists ───────────────────────────────────────────────────────

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

// ── name lists (stream categories, link allow list) ───────────────────────

/** One category name is at most this long. Twitch's own category names top out
 *  well under it; the cap exists so a paste of a whole sentence becomes a
 *  visible refusal rather than a chip nobody can read. */
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

// ── colour ────────────────────────────────────────────────────────────────

/** Accepts `#rgb`, `#rrggbb` and the same two without the hash, in any case.
 *  Anything else falls back rather than painting an embed a colour the
 *  streamer did not choose. */
export function normalizeHex(raw: FieldText, fallback: HexColor = LIVE_COLOR_HEX): HexColor {
  const v = raw.trim().toLowerCase().replace(/^#/, '');
  if (/^[0-9a-f]{3}$/.test(v)) return `#${v[0]}${v[0]}${v[1]}${v[1]}${v[2]}${v[2]}`;
  if (/^[0-9a-f]{6}$/.test(v)) return `#${v}`;
  return fallback;
}

export function isHexColor(raw: FieldText): boolean {
  return HEX6.test(raw.trim().toLowerCase());
}

/**
 * Turns a stored `#rrggbb` into the integer Discord's embed `color` field
 * wants, the same value Go's ddiscord.ParseHexColor produces, or null when
 * there is no colour to send.
 *
 * It exists because the two sides of the ticket-panel embed disagree on the
 * type: the config blob and the colour input hold a hex string, while the
 * `panel` object on `desk.repost` is the wire form of ddiscord.TicketPanelSpec,
 * whose Color is an int. Sending the string instead round-trips through Go's
 * JSON as a type error and the panel is posted with the default colour.
 *
 * Integration fix (2026-09-05): this used to substitute LIVE_COLOR_HEX for a
 * blank field and outgress used to read a zero Color as "unset", so #000000
 * was the one colour the picker could not save -- every repost of a black
 * panel came back brand amber -- and an untouched panel froze today's brand
 * colour into the wire instead of following it. DiscordPanelSpec.Color is a
 * *int now: absent or null means "brand default", 0 means black. So an unset
 * or unparsable hex is null here and the caller OMITS the key; only a colour
 * the streamer actually chose travels, and 0 travels as 0.
 */
export function hexToDiscordColor(raw: FieldText): number | null {
  const v = raw.trim();
  if (v === '' || !HEX_INPUT.test(v)) return null;
  return Number.parseInt(normalizeHex(v).slice(1), 16);
}

// ── ticket panel ──────────────────────────────────────────────────────────

export type TicketPanelSpec = { title: string; body: string; button: string; color: HexColor };

export function ticketPanelSpec(config: DiscordConfig): TicketPanelSpec {
  return {
    title: config.ticketPanelTitle || TICKET_PANEL_DEFAULTS.title,
    body: config.ticketPanelBody || TICKET_PANEL_DEFAULTS.body,
    button: config.ticketPanelButton || TICKET_PANEL_DEFAULTS.button,
    color: normalizeHex(config.ticketPanelColor, TICKET_PANEL_DEFAULTS.color)
  };
}

/** The `panel` object of a `desk.repost` request, matching outgress's
 *  DiscordPanelSpec: the copy defaulted exactly as the preview shows it, and
 *  `color` present ONLY when the streamer picked one (see hexToDiscordColor).
 *  The spec above keeps a hex for the swatch and the preview, which is why
 *  this is a second shape rather than a field on it. */
export type TicketPanelPayload = { title: string; body: string; button: string; color?: number };

/** Built here rather than in the dashboard's store because the console test
 *  runner only executes shared/**: the omission is the behaviour worth
 *  pinning, and a payload assembled in a route file could never be asserted
 *  on. */
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

/** Staff who see and claim tickets: the explicit list when set, otherwise the
 *  three staff role slots — the same fallback the Go accessor uses. */
export function ticketStaffRoleIds(config: DiscordConfig): Snowflake[] {
  const explicit = parseIdList(config.ticketStaffRoleIds);
  if (explicit.length) return explicit;
  return parseIdList([config.ownerRoleId, config.leadModRoleId, config.modsRoleId].join(','));
}

export function ticketLogChannel(config: DiscordConfig): Snowflake {
  return config.ticketLogChannelId || config.logChannelId;
}

// ── validation + merge ────────────────────────────────────────────────────

export type FieldError = { field: keyof DiscordConfig; code: 'snowflake' | 'list' | 'flag' | 'range' | 'color' | 'pinned' | 'length' };

/**
 * What a field is allowed to CONTAIN, checked before any canonicalisation.
 *
 * `color` accepts the shorthand `#abc` and a missing hash because those are
 * shapes `normalizeHex` turns into a real colour; everything else it would
 * silently swap for the fallback, which is a refusal, not a normalisation.
 */
const CHECKS: Record<FieldKind, (field: RuledValue) => boolean> = {
  snowflake: ({ value }) => SNOWFLAKE.test(value),
  snowflakeList: ({ value }) => value.split(',').every((p) => SNOWFLAKE.test(p.trim())),
  flag: ({ value }) => value === 'on' || value === 'off',
  limit: ({ value }) => integerInRange(value, TICKET_OPEN_LIMIT_RANGE),
  color: ({ value }) => HEX_INPUT.test(value.trim()),
  pinned: ({ value }) => value.split(',').every(isPinnedPair),
  text: ({ value, rule }) => value.length <= (rule.max ?? Number.MAX_SAFE_INTEGER)
};

/** The cap a `limit` field is checked against, as the pair it is: the two
 *  bounds were passed as loose numbers to a generic range check, which put
 *  the ticket cap's definition at its one call site instead of next to it. */
const TICKET_OPEN_LIMIT_RANGE: IntRange = {
  min: TICKET_OPEN_LIMIT_MIN,
  max: TICKET_OPEN_LIMIT_MAX
};

/** The colour shapes the editor may submit; `normalizeHex` maps all of them
 *  onto `#rrggbb`. */
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

/** Empty always passes: it is the "unset" value for every field, and the Go
 *  accessors decide the default. */
function accepts(field: RuledValue): boolean {
  if (field.value === '') return true;
  return CHECKS[field.rule.kind](field);
}

/** The refused fields a page can actually point at, split from the ones it
 *  cannot. */
export type RefusedFields = {
  /** Refused fields this config knows, so the page can mark their controls. */
  byField: Partial<Record<keyof DiscordConfig, true>>;
  /** The field a banner should name, or '' when none of them is nameable. */
  first: keyof DiscordConfig | '';
  /** Names that are not fields of this config at all. */
  unknown: string[];
};

/**
 * Turns an `invalid` refusal's `fields[]` into something a form can render.
 *
 * The list crosses the wire from outgress, so it is not trusted to be strings,
 * to be unique, or to name fields that still exist: a field removed from this
 * console but still checked on the Go side would otherwise mark nothing and
 * say nothing, which reads as a save that silently did not take. Anything
 * unrecognised is kept in `unknown` so the page can still say SOMETHING went
 * wrong even when it has no control to put a message under.
 */
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
    const field: RuledValue = { rule: FIELD_RULES[key], value: raw.trim() };
    // Validate the value the form SENT, then normalise -- not the other way
    // round. Both list encoders drop an entry they cannot parse, so
    // normalising first turned `mods=notasnowflake` and `nosuchslot=123` into
    // an empty string that passed validation: the streamer's pick vanished and
    // nothing said so. Raw-first makes the same input a visible FieldError.
    if (!accepts(field)) {
      errors.push({ field: key, code: CODES[field.rule.kind] });
      continue;
    }
    config[key] = normalizeField(field);
  }
  return { config, errors };
}

/** Re-encodes the two list shapes and the colour so the stored string is
 *  canonical whatever spacing or case the form sent. Takes an already-trimmed,
 *  already-validated value. */
function normalizeField(field: RuledValue): FieldText {
  const { rule, value } = field;
  if (rule.kind === 'snowflakeList') return encodeIdList(value.split(','));
  if (rule.kind === 'pinned') return encodePinnedRoles(parsePinnedRoles(value));
  if (rule.kind === 'color' && value !== '') return normalizeHex(value);
  return value;
}

// ── per-guild config rows ─────────────────────────────────────────────────

/**
 * The version a `config.get` reports for a guild that has no row yet.
 *
 * Zero rather than -1 because outgress's `config.set` compares
 * `expected_version` against the stored column, and a row that has never been
 * written has version 0 on the Go side too. Sending -1 for "I have never read
 * this" would make the very first save look like a conflict.
 */
export const DISCORD_CONFIG_VERSION_NEW = 0;

/**
 * Reads the version out of a `config.get` reply.
 *
 * Accepts a string as well as a number: the RPC envelope is JSON and a Go
 * `uint64` marshalled by a service that ever adds `,string` to its tag would
 * arrive quoted. Anything else (missing, negative, fractional, past
 * `Number.MAX_SAFE_INTEGER`) degrades to "never read", which makes the next
 * save a fresh write rather than an unexplained conflict.
 */
export function parseConfigVersion(raw: unknown): number {
  if (isUsableVersionNumber(raw)) return raw;
  if (isDigitString(raw)) {
    const n = Number.parseInt(raw.trim(), 10);
    if (Number.isSafeInteger(n)) return n;
  }
  return DISCORD_CONFIG_VERSION_NEW;
}

/** A version that can be used as it arrived: a whole, non-negative count that
 *  survived the JSON parse exactly. */
function isUsableVersionNumber(raw: unknown): raw is number {
  if (typeof raw !== 'number') return false;
  if (!Number.isSafeInteger(raw)) return false;
  return raw >= 0;
}

/** Digits and nothing else, so a quoted `"12"` is a version while `v12`, an
 *  empty string, and a signed or fractional spelling are not. */
function isDigitString(raw: unknown): raw is string {
  if (typeof raw !== 'string') return false;
  return /^\d+$/.test(raw.trim());
}

// ── guild permissions (the OAuth picker) ──────────────────────────────────

/**
 * The two permissions that let a member add a bot to a guild.
 *
 * BigInt, not Number, and this is not style. Discord serialises a guild's
 * permission bitfield as a DECIMAL STRING in `/users/@me/guilds` precisely
 * because the field outgrew a double: bits past 53 exist today
 * (USE_EXTERNAL_APPS is bit 50, CREATE_EVENTS bit 44), so `parseInt` on a
 * guild that carries a high bit silently rounds and can flip a low bit in the
 * result. The two bits tested here are low, but the parse has to be exact for
 * the AND to mean anything at all.
 */
export const DISCORD_ADMINISTRATOR = 0x8n;
export const DISCORD_MANAGE_GUILD = 0x20n;

export type GuildPermissionEntry = {
  id?: string;
  name?: string;
  owner?: boolean;
  permissions?: string | number;
};

/** Parses Discord's decimal permission string. Anything unparseable is zero:
 *  a guild whose bitfield we cannot read is one we do not offer. */
export function guildPermissionBits(raw: GuildPermissionEntry['permissions']): bigint {
  if (typeof raw === 'number') {
    if (!Number.isSafeInteger(raw) || raw < 0) return 0n;
    return BigInt(raw);
  }
  const v = (raw ?? '').trim();
  if (!/^\d+$/.test(v)) return 0n;
  return BigInt(v);
}

/**
 * Whether this guild may be offered in the picker.
 *
 * Owner wins outright: Discord reports the owner's `permissions` as the
 * everyone-role bitfield in some responses, so an owner with no explicit
 * Administrator role would otherwise be filtered out of their own server.
 */
export function canManageGuild(entry: GuildPermissionEntry): boolean {
  if (entry.owner === true) return true;
  const bits = guildPermissionBits(entry.permissions);
  return (bits & DISCORD_ADMINISTRATOR) !== 0n || (bits & DISCORD_MANAGE_GUILD) !== 0n;
}

// ── guild presentation ────────────────────────────────────────────────────

/**
 * The two-letter tile a guild is drawn with.
 *
 * Not an <img>: the console CSP is `img-src 'self' data:`, so a tag pointed at
 * cdn.discordapp.com renders as a broken box with no fix short of proxying
 * every guild icon through the dashboard, and it would leak the visit to
 * Discord on every page view. Initials of the first two words when there are
 * two, otherwise the first two characters, so "Demo Bakery" reads DB and
 * "Bagels" reads BA.
 */
export function guildMonogram(name: GuildName): string {
  const words = name.trim().split(/\s+/).filter((w) => w !== '');
  if (words.length === 0) return '?';
  if (words.length === 1) return [...words[0]].slice(0, 2).join('').toUpperCase();
  return (([...words[0]][0] ?? '') + ([...words[1]][0] ?? '')).toUpperCase();
}

/**
 * The pill a server card shows.
 *
 * `reauth` outranks `offline`: a guild whose install predates a permission
 * needs the streamer to act, and saying "offline" would send them to wait for
 * a reconnect that already happened.
 *
 * `unknown` outranks BOTH colours. The listing's reauth lookups run after the
 * handler's deadline can pass, and a lookup that failed reports `needsReauth`
 * false -- indistinguishable from a healthy grant. `reauthUnknown` says the
 * flag was never read, so the row shows a neutral pill rather than asserting
 * health (green) or a fault (red) nobody checked.
 */
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

/**
 * The picker's badge.
 *
 * Three states, not two. `mine` is a server this broadcaster has already bound
 * -- clicking through Discord's consent screen again would land back on the
 * same settings page, so the row offers a direct link instead. `elsewhere` is
 * a server outgress refused to bind because it belongs to a different Twitch
 * channel; that row is dead, and offering an install button on it walks the
 * streamer through two Discord screens to reach the same refusal.
 *
 * `elsewhereIds` is what the console LEARNED, not a query: outgress exposes no
 * "who owns this guild" RPC to the dashboard (see the dingress subject list),
 * so the only signal available here is a `bound_elsewhere` the install
 * callback already hit. Anything not in either list is `addable`.
 */
export type GuildPickerBadge = 'mine' | 'elsewhere' | 'addable';

/** The two lists a badge is decided against. They are one input, not two: a
 *  badge read against `bound` without `elsewhere` is a different answer, and
 *  as adjacent same-typed arrays they were transposable at the call site. */
export type GuildPickerLists = {
  /** Guilds this broadcaster has already bound. */
  bound: readonly Snowflake[];
  /** Guilds a `bound_elsewhere` refusal was already collected for. */
  elsewhere?: readonly Snowflake[];
};

export function guildPickerBadge(guildId: Snowflake, lists: GuildPickerLists): GuildPickerBadge {
  if (lists.bound.includes(guildId)) return 'mine';
  if ((lists.elsewhere ?? []).includes(guildId)) return 'elsewhere';
  return 'addable';
}

// ── the guild list Discord returns for a user token ───────────────────────

export type DiscordUserGuild = { id: Snowflake; name: GuildName; owner: boolean; permissions: string };

/**
 * One entry of `/users/@me/guilds`.
 *
 * `permissions` stays the string Discord sent: the bitfield is parsed with
 * BigInt in `guildPermissionBits`, and coercing it to a number here would be
 * the one place the precision is lost.
 */
export function parseUserGuild(raw: unknown): DiscordUserGuild | null {
  if (raw === null || typeof raw !== 'object') return null;
  if (Array.isArray(raw)) return null;
  const g = raw as { id?: unknown; name?: unknown; owner?: unknown; permissions?: unknown };
  if (typeof g.id !== 'string' || g.id === '') return null;
  return {
    id: g.id,
    name: typeof g.name === 'string' ? g.name : '',
    owner: g.owner === true,
    permissions: typeof g.permissions === 'string' ? g.permissions : ''
  };
}

/**
 * A whole page of `/users/@me/guilds`.
 *
 * `null` means "this is not a guild page", which is the case that matters:
 * Discord answers a 429 with a JSON OBJECT (`{message, retry_after}`) and its
 * edge answers an outage with an HTML document, and both used to fall through
 * `Array.isArray` into an empty list that the picker rendered as "you
 * administer no servers". A caller that gets `null` reports the transport
 * failure instead of inventing an answer.
 */
export function parseUserGuilds(raw: unknown): DiscordUserGuild[] | null {
  if (!Array.isArray(raw)) return null;
  const out: DiscordUserGuild[] = [];
  for (const entry of raw) {
    const g = parseUserGuild(entry);
    if (g) out.push(g);
  }
  return out;
}

// ── legacy blob migration ─────────────────────────────────────────────────

/**
 * The config a pre-split board still carries in its per-user modules blob.
 *
 * Before the multi-guild split (§H) the whole config lived in `MOD.discord`,
 * which structurally allowed one server per broadcaster. The blob is narrowed
 * to `{twitchLogin}` on the first save after the split, so anything not copied
 * into the guild row first is lost -- every channel and role id the streamer
 * ever picked. Returns the config to write, or `null` when there is nothing to
 * migrate.
 *
 * The guild id has to match: a blob that names a DIFFERENT server describes a
 * binding this guild's row must not inherit, and one that names no server is
 * either already narrowed or was never set up.
 */
export function legacyConfigFor(blob: unknown, guildId: Snowflake): DiscordConfig | null {
  if (!isSnowflake(guildId)) return null;
  const parsed = parseDiscordConfig(blob);
  if (parsed.guildId !== guildId) return null;
  if (!carriesLegacyFields(parsed)) return null;
  return parsed;
}

/** guildId and twitchLogin survive the narrowing, so a blob holding only those
 *  two is already migrated and copying it over a row would be a no-op write. */
function carriesLegacyFields(config: DiscordConfig): boolean {
  return DISCORD_CONFIG_KEYS.some(
    (key) => key !== 'guildId' && key !== 'twitchLogin' && config[key] !== ''
  );
}
