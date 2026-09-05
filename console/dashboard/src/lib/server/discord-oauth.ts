// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Discord bot-install OAuth. Discord always shows its own confirm; we cannot
// skip that. The callback exchanges the code server-side and takes the guild
// id from the token response, never from the query string: the query
// guild_id is caller-supplied, and trusting it let any signed-in user bind
// (and fill) someone else's server. Only a member who may add bots to a
// guild can obtain a code for it, which is the ownership proof outgress
// relies on.
import { redirect } from '@sveltejs/kit';
import { gateModulePage } from './module-gate';
import { effectiveId } from './board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

// process.env, not $env/dynamic/private, for the module-eval read: this
// file imports module-gate, which sits in the boot import graph.
const DEMO = dev && process.env.DEMO === '1';

export const DISCORD_STATE_COOKIE = 'discord_oauth_state';
// A second cookie for the user-authorization leg. Sharing one with the bot
// install leg would let a stale state from either flow validate the other, and
// the two legs carry different consent: one grants us a read of the visitor's
// guild list, the other adds a bot to a server.
export const DISCORD_PICK_STATE_COOKIE = 'discord_pick_state';
export const DISCORD_STATE_TTL_SECONDS = 600;

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
// ../../../../shared/lib/discord-permissions.test.ts recomputes the Go
// expression from source and fails on any drift.
export const DISCORD_BOT_PERMISSIONS = 1102012607574;

const TOKEN_URL = 'https://discord.com/api/v10/oauth2/token';
const USER_GUILDS_URL = 'https://discord.com/api/v10/users/@me/guilds';
const TOKEN_TIMEOUT_MS = 8000;
const GUILDS_TIMEOUT_MS = 8000;

export function requireDiscordActor(locals: App.Locals): string {
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

export function discordInviteURL(state: string): string {
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
export function discordUserAuthURL(state: string): string {
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
export function discordInstallURL(state: string, guildId: string): string {
  const base = discordInviteURL(state);
  if (!base || !guildId) return base;
  const u = new URL(base);
  u.searchParams.set('guild_id', guildId);
  u.searchParams.set('disable_guild_select', 'true');
  return u.toString();
}

export type DiscordUserGuild = { id: string; name: string; owner: boolean; permissions: string };

/**
 * Exchanges the user-authorization code and lists that user's guilds.
 *
 * The access token exists only inside this function: it is not returned, not
 * stored, and not logged, so a leak would need someone to change this file.
 * Returns an empty list on any failure; the caller turns that into a named
 * redirect rather than a stack trace with a token in it.
 */
export async function listUserGuilds(code: string): Promise<DiscordUserGuild[]> {
  const token = await exchangeUserCode(code);
  if (!token) return [];
  const res = await fetch(USER_GUILDS_URL, {
    headers: { Authorization: `Bearer ${token}` },
    signal: AbortSignal.timeout(GUILDS_TIMEOUT_MS)
  });
  if (!res.ok) return [];
  const json: unknown = await res.json();
  if (!Array.isArray(json)) return [];
  return json.map(userGuild).filter((g): g is DiscordUserGuild => g !== null);
}

function userGuild(raw: unknown): DiscordUserGuild | null {
  if (raw === null || typeof raw !== 'object') return null;
  const g = raw as { id?: unknown; name?: unknown; owner?: unknown; permissions?: unknown };
  if (typeof g.id !== 'string' || g.id === '') return null;
  return {
    id: g.id,
    name: typeof g.name === 'string' ? g.name : '',
    owner: g.owner === true,
    // Kept as the string Discord sent: the bitfield is parsed with BigInt in
    // shared/discord-config, and coercing it to a number here would be the one
    // place the precision is lost.
    permissions: typeof g.permissions === 'string' ? g.permissions : ''
  };
}

async function exchangeUserCode(code: string): Promise<string> {
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
export async function exchangeInstallCode(code: string): Promise<string> {
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
