// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { parseTokenRequest, type TokenParseError } from '#lib/server/device-auth-validate';
import { checkTokenRate } from '#lib/server/device-auth-store';
import { deviceCodeExchange, deviceTokenRefresh, type DeviceTokens, type TokenOutcome } from '#lib/server/services';

// The desktop app's native HTTP client, never a browser: no cookies, no CORS headers, form body only.
const MAX_BODY_BYTES = 4096;

const NO_STORE = {
  'Cache-Control': 'no-store',
  Pragma: 'no-cache',
  'Content-Type': 'application/json; charset=utf-8'
} as const;

function oauthError(status: number, error: string): Response {
  return new Response(JSON.stringify({ error }), { status, headers: NO_STORE });
}

function tooLarge(): Response {
  return oauthError(400, 'invalid_request');
}

function unsupportedContentType(request: Request): boolean {
  const type = (request.headers.get('content-type') ?? '').split(';', 1)[0].trim().toLowerCase();
  return type !== 'application/x-www-form-urlencoded';
}

function bodyOversized(request: Request): boolean {
  const len = Number(request.headers.get('content-length') ?? '0');
  return Number.isFinite(len) && len > MAX_BODY_BYTES;
}

function tokenResponse(tokens: DeviceTokens): Response {
  return new Response(
    JSON.stringify({
      access_token: tokens.accessToken,
      token_type: tokens.tokenType,
      expires_in: tokens.expiresIn,
      refresh_token: tokens.refreshToken,
      grant_id: tokens.grantId,
      device_id: tokens.deviceId
    }),
    { status: 200, headers: NO_STORE }
  );
}

// A transient Users outage must read as a retryable server_error, never invalid_grant:
// the app would otherwise discard a still-good refresh token on a blip.
function responseFor(outcome: TokenOutcome): Response {
  if (outcome.ok) return tokenResponse(outcome.tokens);
  return oauthError(outcome.oauthError === 'server_error' ? 503 : 400, outcome.oauthError);
}

async function exchange(args: { code: string; codeVerifier: string; redirectUri: string }): Promise<Response> {
  return responseFor(await deviceCodeExchange(args));
}

async function refresh(refreshToken: string): Promise<Response> {
  return responseFor(await deviceTokenRefresh(refreshToken));
}

function rateKeyOf(parsed: ReturnType<typeof parseTokenRequest>): string | null {
  if (!parsed.ok) return null;
  return parsed.value.grantType === 'authorization_code' ? parsed.value.code : parsed.value.refreshToken;
}

const PARSE_STATUS: Record<TokenParseError, number> = {
  invalid_request: 400,
  invalid_client: 400,
  unsupported_grant_type: 400
};

export const POST: RequestHandler = async ({ request }) => {
  if (unsupportedContentType(request)) return oauthError(400, 'invalid_request');
  if (bodyOversized(request)) return tooLarge();

  const body = await request.text();
  if (Buffer.byteLength(body) > MAX_BODY_BYTES) return tooLarge();

  const parsed = parseTokenRequest(new URLSearchParams(body));
  if (!parsed.ok) return oauthError(PARSE_STATUS[parsed.error], parsed.error);

  const rateKey = rateKeyOf(parsed);
  if (rateKey) {
    const limited = await checkTokenRate(rateKey);
    if (!limited.allowed) {
      return new Response(JSON.stringify({ error: 'invalid_request', error_description: 'rate limited' }), {
        status: 429,
        headers: { ...NO_STORE, 'Retry-After': String(limited.retryAfterSec) }
      });
    }
  }

  return parsed.value.grantType === 'authorization_code'
    ? exchange(parsed.value)
    : refresh(parsed.value.refreshToken);
};
