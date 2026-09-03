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
export const DISCORD_STATE_TTL_SECONDS = 600;

// BotPermissions matches internal/domain/discord.BotPermissions: kick, ban,
// manage channels, reactions, view, send, manage messages, embed, attach,
// history, connect, move members, manage roles, slash commands, timeout.
// No Administrator. Bit 40 (MODERATE_MEMBERS) is still a safe JS integer.
export const DISCORD_BOT_PERMISSIONS = 1101945498710;

const TOKEN_URL = 'https://discord.com/api/v10/oauth2/token';
const TOKEN_TIMEOUT_MS = 8000;

export function requireDiscordActor(locals: App.Locals): string {
  gateModulePage(locals.session, 'discord');
  const uid = !DEMO && !locals.session ? null : effectiveId(locals.session);
  if (!uid) throw redirect(302, '/login?next=/discord');
  if (DEMO) throw redirect(302, '/discord');
  return uid;
}

export function discordFail(slug: string): never {
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
