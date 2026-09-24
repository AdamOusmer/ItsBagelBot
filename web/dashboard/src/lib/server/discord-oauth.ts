// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { redirect, type Cookies } from '@sveltejs/kit';
import { gateModulePage } from './module-gate';
import { effectiveId } from './board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { decodeKey } from '@bagel/kit/server/session';
import { openOAuthState, sealOAuthState } from '@bagel/kit/server/oauth-state';
import { encodeIdList, parseIdList, parseUserGuilds, type DiscordUserGuild } from '@bagel/kit';

export type { DiscordUserGuild };

export type OAuthState = string;
export type OAuthCode = string;
export type GuildId = string;
export type UserId = string;
type AccessToken = string;

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
const DEMO = dev && process.env.DEMO === '1';

// One cookie and HMAC label per leg: a shared one lets either flow's state validate the other.
const INSTALL_COOKIE = 'discord_oauth_state';
const PICK_COOKIE = 'discord_pick_state';
const INSTALL_LABEL = 'discord-install';
const PICK_LABEL = 'discord-pick';
export const DISCORD_STATE_TTL_SECONDS = 600;

const STATE_PATH = '/discord';

function stateCookieName(leg: Leg, secure: boolean): string {
  return secure ? `__Secure-${leg.cookie}` : leg.cookie;
}

function stateKey(): Buffer {
  return decodeKey(process.env.SESSION_KEY);
}

export type Leg = { cookie: string; label: string };

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

export function takeDiscordState(ref: DiscordStateRef): OAuthState {
  const { cookies, url, leg, uid } = ref;
  const secure = url.protocol === 'https:';
  const name = stateCookieName(leg, secure);
  const raw = cookies.get(name) ?? '';
  for (const n of [name, leg.cookie]) cookies.delete(n, { path: STATE_PATH, secure });
  if (!raw) return '';
  return openOAuthState(stateKey(), leg.label, uid, raw) ?? '';
}

export function discordStateOK(ref: DiscordStateRef): boolean {
  const stored = takeDiscordState(ref);
  const echoed = ref.url.searchParams.get('state') ?? '';
  return stored !== '' && echoed !== '' && stored === echoed;
}

const BLOCKED_COOKIE = 'discord_blocked_guilds';

const BLOCKED_TTL_SECONDS = 1800;

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

export function discordPickRedirectURI(): string {
  const explicit = (env.DISCORD_PICK_REDIRECT_URI || '').trim();
  if (explicit) return explicit;
  const base = discordRedirectURI();
  if (!base) return '';
  return base.replace(/\/callback\/?$/, '/pick');
}

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

export function discordInstallURL(state: OAuthState, guildId: GuildId): string {
  const base = discordInviteURL(state);
  if (!base || !guildId) return base;
  const u = new URL(base);
  u.searchParams.set('guild_id', guildId);
  u.searchParams.set('disable_guild_select', 'true');
  return u.toString();
}

const GUILDS_PAGE_LIMIT = 200;

const GUILDS_MAX_PAGES = 10;

export type UserGuildsResult =
  | { ok: true; guilds: DiscordUserGuild[] }
  | { ok: false; code: Extract<DiscordErrorSlug, 'oauth' | 'rate_limited' | 'discord_unavailable'> };

export async function listUserGuilds(code: OAuthCode): Promise<UserGuildsResult> {
  const token = await exchangeUserCode(code);
  if (!token) return { ok: false, code: 'oauth' };
  const guilds: DiscordUserGuild[] = [];
  let after = '';
  for (let page = 0; page < GUILDS_MAX_PAGES; page++) {
    const res = await fetchGuildPage(token, after);
    if (!res.ok) return res;
    guilds.push(...res.guilds);
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
