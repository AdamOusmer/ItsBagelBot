// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Nightbot OAuth for the config importer: plain authorization_code, no OIDC —
// Nightbot issues opaque access tokens and no id_token, so the oauth4webapi
// machinery the Twitch login uses would be dead weight here. The token is used
// once (fetch commands/timers/spam protection during preview), rides a
// short-lived HttpOnly cookie between callback and preview, and is dropped on
// commit; the refresh token is never requested off the response.
//
// App registration lives at nightbot.tv/account/applications; the redirect
// URI registered there must byte-match NIGHTBOT_REDIRECT_URI (Doppler).
import { env } from '$env/dynamic/private';

const AUTHORIZE_URL = 'https://api.nightbot.tv/oauth2/authorize';
const TOKEN_URL = 'https://api.nightbot.tv/oauth2/token';

// commands + timers are what the importer reads; spam_protection adds the
// blacklist terms the parser folds into automod. All three are read-only in
// practice (the importer only GETs), but Nightbot scopes are not split by verb.
export const NIGHTBOT_SCOPES = 'commands timers spam_protection';

// Cookie names shared by the connect/callback routes and the import actions.
// Both are HttpOnly and path-scoped to /settings/import.
export const NB_STATE_COOKIE = 'nb_oauth_state';
export const NB_TOKEN_COOKIE = 'nb_import_token';
export const NB_COOKIE_PATH = '/settings/import';

// Token cookie lifetime: the wizard round trip is minutes, so 15 minutes
// bounds how long a captured cookie stays useful without making a slow
// review step re-run the consent.
export const NB_TOKEN_TTL_SECONDS = 900;

const EXCHANGE_TIMEOUT_MS = 10_000;

interface NightbotApp {
  clientId: string;
  clientSecret: string;
  redirectUri: string;
}

function app(): NightbotApp {
  const clientId = env.NIGHTBOT_CLIENT_ID ?? '';
  const clientSecret = env.NIGHTBOT_CLIENT_SECRET ?? '';
  const redirectUri = env.NIGHTBOT_REDIRECT_URI ?? '';
  if (!clientId || !clientSecret || !redirectUri)
    throw new Error('NIGHTBOT_CLIENT_ID/SECRET/REDIRECT_URI not set');
  return { clientId, clientSecret, redirectUri };
}

// nightbotConfigured lets the routes degrade to a readable error instead of a
// 500 when the app registration has not landed in this environment yet.
export function nightbotConfigured(): boolean {
  return !!(env.NIGHTBOT_CLIENT_ID && env.NIGHTBOT_CLIENT_SECRET && env.NIGHTBOT_REDIRECT_URI);
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

// exchangeNightbotCode swaps the callback code for an access token.
// client_secret_post (credentials in the form body) matches Nightbot's
// documented token request. Any failure throws with the upstream status so the
// callback can log it; the user-facing path is one generic retry message.
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
