// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Discord bot-install OAuth. Discord always shows its own confirm; we cannot
// skip that. The callback exchanges the code server-side and takes the guild
// id from the token response, never from the query string: the query
// guild_id is caller-supplied, and trusting it let any signed-in user bind
// (and fill) someone else's server. Only a member who may add bots to a
// guild can obtain a code for it, which is the ownership proof outgress
// relies on.
import { redirect, type Cookies } from '@sveltejs/kit';
import { gateModulePage } from './module-gate';
import { effectiveId } from './board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { decodeKey } from '@bagel/shared/server/session';
import { openOAuthState, sealOAuthState } from '@bagel/shared/server/oauth-state';
import { encodeIdList, parseIdList, parseUserGuilds, type DiscordUserGuild } from '@bagel/shared';

export type { DiscordUserGuild };

/**
 * Names for the strings this flow shuffles between Discord and us.
 *
 * Every one of them is a bare string on the wire, and the mistakes this file
 * exists to prevent are all one string standing in for another: echoing the
 * state where the code belongs, binding the query `guild_id` instead of the
 * one the token response names, letting a user access token escape into a
 * return value. A signature that says `guildId: GuildId` says which string it
 * wants; five parameters typed `string` say nothing.
 *
 * Aliases rather than branded types, deliberately: every caller lives in a
 * `+server.ts` that reads these values off a URL or a cookie as plain strings,
 * so a nominal brand would buy one cast per read and no extra safety at the
 * boundary that actually matters -- which is this file refusing to trust the
 * query string at all.
 */
export type OAuthState = string;
export type OAuthCode = string;
export type GuildId = string;
export type UserId = string;
/** A Discord user access token. It is never returned to a caller, never
 *  persisted and never logged; see listUserGuilds. */
type AccessToken = string;

// process.env, not $env/dynamic/private, for the module-eval read: this
// file imports module-gate, which sits in the boot import graph.
const DEMO = dev && process.env.DEMO === '1';

// Two cookies, and two HMAC labels, for the two legs. Sharing one would let a
// stale state from either flow validate the other, and the two legs carry
// different consent: one grants us a read of the visitor's guild list, the
// other adds a bot to a server.
const INSTALL_COOKIE = 'discord_oauth_state';
const PICK_COOKIE = 'discord_pick_state';
const INSTALL_LABEL = 'discord-install';
const PICK_LABEL = 'discord-pick';
export const DISCORD_STATE_TTL_SECONDS = 600;

/**
 * The state cookie's path, and why it is not `__Host-`.
 *
 * `__Host-` would be the stronger prefix, but RFC 6265bis §4.1.3.2 makes it
 * MANDATE `Path=/`, and these cookies have no business riding on every request
 * to the dashboard -- they exist for two redirects under /discord. `__Secure-`
 * buys the half that matters against a network attacker (never settable over
 * plain http, never from a non-secure origin) and puts no constraint on the
 * path, so that is the trade taken. The prefix has to be dropped entirely over
 * http or the browser refuses the cookie, which is local dev only.
 */
const STATE_PATH = '/discord';

function stateCookieName(leg: Leg, secure: boolean): string {
  return secure ? `__Secure-${leg.cookie}` : leg.cookie;
}

/** The app's own SESSION_KEY. Read per call, never at module eval: this file
 *  sits on the boot import graph. */
function stateKey(): Buffer {
  return decodeKey(process.env.SESSION_KEY);
}

export type Leg = { cookie: string; label: string };

/**
 * One state cookie's address: the jar it lives in, the request URL that
 * decides whether it is a `__Secure-` cookie, which leg of the flow it
 * belongs to, and the user it is sealed to.
 *
 * The four are one thing, not four arguments. Every function below needs all
 * of them, three of the four are needed only to derive the cookie name and
 * the seal, and as a positional list `(cookies, url, leg, uid)` put two
 * opaque values (leg, uid) next to each other where a transposition still
 * type-checks against nothing. Passing them named also leaves room for the
 * one value that genuinely differs per call -- the state itself -- to stay a
 * separate argument.
 */
export type DiscordStateRef = {
  cookies: Cookies;
  url: URL;
  leg: Leg;
  uid: UserId;
};

const INSTALL_LEG: Leg = { cookie: INSTALL_COOKIE, label: INSTALL_LABEL };
const PICK_LEG: Leg = { cookie: PICK_COOKIE, label: PICK_LABEL };

export const DISCORD_INSTALL_LEG = INSTALL_LEG;
export const DISCORD_PICK_LEG = PICK_LEG;

/**
 * Mints a state, seals it to this signed-in user, and stores the cookie.
 *
 * The returned value is what goes to Discord; the cookie holds the same state
 * plus an HMAC over (leg, uid, state), so a cookie planted by one account
 * cannot be redeemed by another. See @bagel/shared/server/oauth-state.
 */
export function putDiscordState(ref: DiscordStateRef, state: OAuthState): void {
  const { cookies, url, leg, uid } = ref;
  const secure = url.protocol === 'https:';
  cookies.set(stateCookieName(leg, secure), sealOAuthState(stateKey(), leg.label, uid, state), {
    path: STATE_PATH,
    httpOnly: true,
    secure,
    sameSite: 'lax',
    maxAge: DISCORD_STATE_TTL_SECONDS
  });
}

/**
 * Reads the state back, one time, and clears the cookie whatever happens.
 *
 * Returns '' when the cookie is absent or was not minted for this leg and this
 * user; the caller turns that into a named redirect. Both spellings are
 * deleted because a deployment that changed protocol (or a developer moving
 * between the two) can leave the other one behind.
 */
export function takeDiscordState(ref: DiscordStateRef): OAuthState {
  const { cookies, url, leg, uid } = ref;
  const secure = url.protocol === 'https:';
  const name = stateCookieName(leg, secure);
  const raw = cookies.get(name) ?? '';
  for (const n of [name, leg.cookie]) cookies.delete(n, { path: STATE_PATH, secure });
  if (!raw) return '';
  return openOAuthState(stateKey(), leg.label, uid, raw) ?? '';
}

/**
 * The whole state check for one callback: the cookie has to exist, be sealed
 * to this user, and match the state Discord echoed back.
 */
export function discordStateOK(ref: DiscordStateRef): boolean {
  const stored = takeDiscordState(ref);
  const echoed = ref.url.searchParams.get('state') ?? '';
  return stored !== '' && echoed !== '' && stored === echoed;
}

// ── servers that belong to somebody else ──────────────────────────────────

const BLOCKED_COOKIE = 'discord_blocked_guilds';

/**
 * How long the picker remembers a `bound_elsewhere` refusal.
 *
 * The dashboard has no way to ASK who owns a guild -- outgress serves no such
 * subject to it -- so the only signal available is a refusal this browser
 * already collected. Thirty minutes covers the "try it, get refused, come
 * back and try the next one" loop that the memory exists to stop, and is short
 * enough that a genuine hand-over (the other channel disconnects) is not
 * remembered as permanent.
 */
const BLOCKED_TTL_SECONDS = 1800;

/** Cap on the remembered list. A cookie is a request-header budget, and nobody
 *  walks past a handful of refusals before asking why. */
const BLOCKED_MAX = 8;

export function rememberBoundElsewhere(cookies: Cookies, url: URL, guildId: GuildId): void {
  const next = encodeIdList([guildId, ...boundElsewhereIds(cookies)].slice(0, BLOCKED_MAX));
  if (!next) return;
  cookies.set(BLOCKED_COOKIE, next, {
    path: STATE_PATH,
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: BLOCKED_TTL_SECONDS
  });
}

export function boundElsewhereIds(cookies: Cookies): GuildId[] {
  return parseIdList(cookies.get(BLOCKED_COOKIE) ?? '');
}

// BotPermissions matches internal/domain/discord.BotPermissions: kick, ban,
// manage channels, reactions, view, send, manage messages, embed, attach,
// history, connect, move members, manage roles, slash commands, timeout,
// change nickname. No Administrator. Bit 40 (MODERATE_MEMBERS) is still a
// safe JS integer.
//
// This literal and the Go bit expression are the same number in two
// languages, and this file is the one that actually mints the invite URL a
// streamer clicks. If it lags the Go side, every guild installed after the
// change is missing a permission the services assume they have, and Discord
// freezes that into the bot's role at install so it never self-heals.
// ../../../../kit/lib/discord-permissions.test.ts recomputes the Go
// expression from source and fails on any drift.
export const DISCORD_BOT_PERMISSIONS = 1102012607574;

const TOKEN_URL = 'https://discord.com/api/v10/oauth2/token';
const USER_GUILDS_URL = 'https://discord.com/api/v10/users/@me/guilds';
const TOKEN_TIMEOUT_MS = 8000;
const GUILDS_TIMEOUT_MS = 8000;

export function requireDiscordActor(locals: App.Locals): UserId {
  gateModulePage(locals.session, 'discord');
  const uid = !DEMO && !locals.session ? null : effectiveId(locals.session);
  if (!uid) throw redirect(302, '/login?next=/discord');
  if (DEMO) throw redirect(302, '/discord');
  return uid;
}

/**
 * The `?e=` slugs the page knows how to explain.
 *
 * The first four are OAuth-flow-local — they describe something that went
 * wrong before any RPC — and the rest are the dingress refusal codes verbatim,
 * so a redirect that carries a refusal out of the callback names it with the
 * same word the RPC reply used. The page maps each to a message through a
 * typed literal map, so a slug added here without copy fails the i18n scan.
 */
export const DISCORD_ERROR_SLUGS = [
  'oauth',
  'unconfigured',
  'setup',
  'state',
  'noguilds',
  'conflict',
  'bound_elsewhere',
  'not_bound',
  'discord_unavailable',
  'forbidden',
  'rate_limited',
  'invalid'
] as const;

export type DiscordErrorSlug = (typeof DISCORD_ERROR_SLUGS)[number];

export function discordFail(slug: DiscordErrorSlug): never {
  throw redirect(302, `/discord?e=${slug}`);
}

export function discordClientId(): string {
  return (env.DISCORD_CLIENT_ID || '').trim();
}

function discordClientSecret(): string {
  return (env.DISCORD_CLIENT_SECRET || '').trim();
}

export function discordRedirectURI(): string {
  return (env.DISCORD_REDIRECT_URI || '').trim();
}

export function discordTemplateCode(): string {
  return (env.DISCORD_TEMPLATE_CODE || '').trim();
}

export function discordConfigured(): boolean {
  return discordClientId() !== '' && discordClientSecret() !== '' && discordRedirectURI() !== '';
}

export function discordInviteURL(state: OAuthState): string {
  const clientId = discordClientId();
  const redirect = discordRedirectURI();
  if (!clientId || !redirect) return '';
  const u = new URL('https://discord.com/oauth2/authorize');
  u.searchParams.set('client_id', clientId);
  u.searchParams.set('permissions', String(DISCORD_BOT_PERMISSIONS));
  u.searchParams.set('scope', 'bot applications.commands');
  u.searchParams.set('redirect_uri', redirect);
  u.searchParams.set('response_type', 'code');
  if (state) u.searchParams.set('state', state);
  return u.toString();
}

/**
 * Where Discord sends the visitor back after the USER-authorization leg.
 *
 * Derived from the bot-install redirect by swapping the last segment, so a
 * deployment configures one variable and gets both. DISCORD_PICK_REDIRECT_URI
 * overrides it for a deployment whose two URIs are not siblings. Either way
 * the value has to be registered in the Discord application, exactly, or
 * Discord refuses the authorize call with its own error page.
 */
export function discordPickRedirectURI(): string {
  const explicit = (env.DISCORD_PICK_REDIRECT_URI || '').trim();
  if (explicit) return explicit;
  const base = discordRedirectURI();
  if (!base) return '';
  return base.replace(/\/callback\/?$/, '/pick');
}

/**
 * Step one: ask the visitor which servers they are in.
 *
 * `identify guilds` is read-only and adds nothing to any server. The token it
 * yields is used once, in the callback, to list guilds; it is never persisted
 * and never logged. We ask before the install so the picker can show real
 * server names instead of dropping the streamer into Discord's own guild
 * dropdown, which lists servers Bagel can never be added to.
 */
export function discordUserAuthURL(state: OAuthState): string {
  const clientId = discordClientId();
  const redirect = discordPickRedirectURI();
  if (!clientId || !redirect) return '';
  const u = new URL('https://discord.com/oauth2/authorize');
  u.searchParams.set('client_id', clientId);
  u.searchParams.set('scope', 'identify guilds');
  u.searchParams.set('redirect_uri', redirect);
  u.searchParams.set('response_type', 'code');
  if (state) u.searchParams.set('state', state);
  return u.toString();
}

/**
 * Step two: install the bot into the guild the streamer picked.
 *
 * guild_id preselects it and disable_guild_select stops Discord offering the
 * dropdown again, so the server named on our picker is the server that gets
 * the bot. The id is still not trusted afterwards: the callback reads the
 * bound guild out of the token response (see exchangeInstallCode).
 */
export function discordInstallURL(state: OAuthState, guildId: GuildId): string {
  const base = discordInviteURL(state);
  if (!base || !guildId) return base;
  const u = new URL(base);
  u.searchParams.set('guild_id', guildId);
  u.searchParams.set('disable_guild_select', 'true');
  return u.toString();
}

/**
 * Discord's page size for /users/@me/guilds. The endpoint defaults to 200 and
 * caps there; asking for more is refused, and asking for less only means more
 * round trips. A user in more than 200 servers is not exotic (Discord's own
 * limit is 100 without Nitro and 200 with it, and a bot-heavy account with
 * multiple logins routinely sits near it), and before pagination those
 * streamers simply could not see their own server in the picker.
 */
const GUILDS_PAGE_LIMIT = 200;

/**
 * A hard stop on the walk. 10 pages is 2000 guilds, an order of magnitude past
 * Discord's own per-user ceiling, so hitting it means the endpoint is looping
 * rather than that somebody is popular -- and an unbounded `after` loop on a
 * misbehaving upstream is a hung request holding a user token in memory.
 */
const GUILDS_MAX_PAGES = 10;

/**
 * Why listUserGuilds failed, in the same words as the `?e=` slugs.
 *
 * It used to return an empty array for every failure, so a 429 and an outage
 * both rendered "you do not administer any servers" -- a sentence that tells
 * the streamer to go fix their Discord permissions when the truth is "try
 * again in a minute".
 */
export type UserGuildsResult =
  | { ok: true; guilds: DiscordUserGuild[] }
  | { ok: false; code: Extract<DiscordErrorSlug, 'oauth' | 'rate_limited' | 'discord_unavailable'> };

/**
 * Exchanges the user-authorization code and lists that user's guilds.
 *
 * The access token exists only inside this function: it is not returned, not
 * stored, and not logged, so a leak would need someone to change this file.
 */
export async function listUserGuilds(code: OAuthCode): Promise<UserGuildsResult> {
  const token = await exchangeUserCode(code);
  if (!token) return { ok: false, code: 'oauth' };
  const guilds: DiscordUserGuild[] = [];
  let after = '';
  for (let page = 0; page < GUILDS_MAX_PAGES; page++) {
    const res = await fetchGuildPage(token, after);
    if (!res.ok) return res;
    guilds.push(...res.guilds);
    // A short page is the last page. Discord gives no cursor of its own here;
    // `after` is the last id seen, and ids are snowflakes so the order is
    // stable across the walk.
    if (res.guilds.length < GUILDS_PAGE_LIMIT) return { ok: true, guilds };
    after = res.guilds[res.guilds.length - 1].id;
  }
  return { ok: true, guilds };
}

async function fetchGuildPage(token: AccessToken, after: GuildId): Promise<UserGuildsResult> {
  const u = new URL(USER_GUILDS_URL);
  u.searchParams.set('limit', String(GUILDS_PAGE_LIMIT));
  if (after) u.searchParams.set('after', after);
  const res = await fetch(u, {
    headers: { Authorization: `Bearer ${token}` },
    signal: AbortSignal.timeout(GUILDS_TIMEOUT_MS)
  }).catch(() => null);
  if (!res) return { ok: false, code: 'discord_unavailable' };
  if (res.status === 429) return { ok: false, code: 'rate_limited' };
  if (!res.ok) return { ok: false, code: 'discord_unavailable' };
  const json: unknown = await res.json().catch(() => null);
  // parseUserGuilds returns null for every body that is not a guild page --
  // the 429 JSON object, an edge HTML page -- so those never masquerade as an
  // empty list. Tested in shared/lib/discord-config.test.ts.
  const guilds = parseUserGuilds(json);
  if (!guilds) return { ok: false, code: 'discord_unavailable' };
  return { ok: true, guilds };
}

async function exchangeUserCode(code: OAuthCode): Promise<AccessToken> {
  const res = await fetch(TOKEN_URL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      client_id: discordClientId(),
      client_secret: discordClientSecret(),
      grant_type: 'authorization_code',
      code,
      redirect_uri: discordPickRedirectURI()
    }),
    signal: AbortSignal.timeout(TOKEN_TIMEOUT_MS)
  });
  if (!res.ok) return '';
  const json = (await res.json()) as { access_token?: unknown };
  return typeof json.access_token === 'string' ? json.access_token : '';
}

export function discordTemplateURL(): string {
  const code = discordTemplateCode();
  return code ? `https://discord.new/${code}` : '';
}

// exchangeInstallCode redeems the bot-install code. For the bot scope the
// token response carries the guild the user authorised; that id is the only
// one the callback may bind. Returns '' when Discord rejects the code or
// the response names no guild.
export async function exchangeInstallCode(code: OAuthCode): Promise<GuildId> {
  const body = new URLSearchParams({
    client_id: discordClientId(),
    client_secret: discordClientSecret(),
    grant_type: 'authorization_code',
    code,
    redirect_uri: discordRedirectURI()
  });
  const res = await fetch(TOKEN_URL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body,
    signal: AbortSignal.timeout(TOKEN_TIMEOUT_MS)
  });
  if (!res.ok) return '';
  const json = (await res.json()) as { guild?: { id?: unknown } };
  const id = json.guild?.id;
  return typeof id === 'string' ? id.trim() : '';
}
