// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { env } from '$env/dynamic/private';

const AUTHORIZE_URL = 'https://api.nightbot.tv/oauth2/authorize';
const TOKEN_URL = 'https://api.nightbot.tv/oauth2/token';

export const NIGHTBOT_SCOPES = 'commands timers spam_protection';

export const NB_STATE_COOKIE = 'nb_oauth_state';
export const NB_TOKEN_COOKIE = 'nb_import_token';
export const NB_RETURN_COOKIE = 'nb_import_return';
export const NB_STATE_COOKIE_PATH = '/settings/import';
export const NB_COOKIE_PATH = '/';

export const NB_TOKEN_TTL_SECONDS = 900;

const EXCHANGE_TIMEOUT_MS = 10_000;

interface NightbotApp {
  clientId: string;
  clientSecret: string;
  redirectUri: string;
}

function readApp(): NightbotApp | null {
  const app: NightbotApp = {
    clientId: env.NIGHTBOT_CLIENT_ID ?? '',
    clientSecret: env.NIGHTBOT_CLIENT_SECRET ?? '',
    redirectUri: env.NIGHTBOT_REDIRECT_URI ?? ''
  };
  return Object.values(app).every((v) => v !== '') ? app : null;
}

function app(): NightbotApp {
  const a = readApp();
  if (!a) throw new Error('NIGHTBOT_CLIENT_ID/SECRET/REDIRECT_URI not set');
  return a;
}

export function nightbotConfigured(): boolean {
  return readApp() !== null;
}

export function importOwner(locals: App.Locals): boolean {
  const s = locals.session;
  return !!s && !s.delegate_of && !s.impersonator_id;
}

export function nightbotAuthURL(state: string): URL {
  const a = app();
  const url = new URL(AUTHORIZE_URL);
  url.searchParams.set('response_type', 'code');
  url.searchParams.set('client_id', a.clientId);
  url.searchParams.set('redirect_uri', a.redirectUri);
  url.searchParams.set('scope', NIGHTBOT_SCOPES);
  url.searchParams.set('state', state);
  return url;
}

export async function exchangeNightbotCode(code: string): Promise<string> {
  const a = app();
  const body = new URLSearchParams({
    client_id: a.clientId,
    client_secret: a.clientSecret,
    grant_type: 'authorization_code',
    redirect_uri: a.redirectUri,
    code
  });
  const res = await fetch(TOKEN_URL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json' },
    body,
    signal: AbortSignal.timeout(EXCHANGE_TIMEOUT_MS)
  });
  if (!res.ok) throw new Error(`nightbot token endpoint returned ${res.status}`);
  const doc = (await res.json()) as { access_token?: unknown };
  if (typeof doc.access_token !== 'string' || doc.access_token === '')
    throw new Error('nightbot token response carried no access_token');
  return doc.access_token;
}
