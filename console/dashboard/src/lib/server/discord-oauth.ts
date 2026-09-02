// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Discord bot-install OAuth. Discord always shows its own confirm; we cannot
// skip that. The callback only needs guild_id — the bot token is fleet-wide
// env, so we never exchange the code.
import { redirect } from '@sveltejs/kit';
import { gateModulePage } from './module-gate';
import { effectiveId } from './board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

const DEMO = dev && env.DEMO === '1';

export const DISCORD_STATE_COOKIE = 'discord_oauth_state';
export const DISCORD_STATE_TTL_SECONDS = 600;

// BotPermissions matches internal/domain/discord.BotPermissions: send,
// embed, attach, view, history, reactions, manage channels, manage roles,
// connect, move members, slash commands. No Administrator.
export const DISCORD_BOT_PERMISSIONS = 2433862736;

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

export function discordRedirectURI(): string {
  return (env.DISCORD_REDIRECT_URI || '').trim();
}

export function discordTemplateCode(): string {
  return (env.DISCORD_TEMPLATE_CODE || '').trim();
}

export function discordConfigured(): boolean {
  return discordClientId() !== '' && discordRedirectURI() !== '';
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
