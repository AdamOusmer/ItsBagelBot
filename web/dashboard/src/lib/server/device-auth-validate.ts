// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const DESKTOP_CLIENT_ID = 'itsbagelbot-desktop';

export const PLATFORMS = ['macos', 'windows', 'linux'] as const;
export type Platform = (typeof PLATFORMS)[number];

const MIN_PORT = 1024;
const MAX_PORT = 65535;
const STATE_MAX = 512;
const DEVICE_NAME_MAX = 64;

// S256 challenge is base64url(sha256(verifier)): always 43 characters, no padding.
const CODE_CHALLENGE_RE = /^[A-Za-z0-9_-]{43}$/;
// RFC 7636 unreserved charset, 43-128 characters.
const CODE_VERIFIER_RE = /^[A-Za-z0-9._~-]{43,128}$/;
const CODE_RE = /^[A-Za-z0-9_-]{1,512}$/;
const REFRESH_TOKEN_RE = /^bbr_[A-Za-z0-9_-]{1,512}$/;
// Bidi overrides are format characters; on the consent page they would reorder what the user reads.
const UNSAFE_NAME_CHARS = /[\p{Cc}\p{Cf}]/gu;

export function validClientId(value: string | null): value is typeof DESKTOP_CLIENT_ID {
  return value === DESKTOP_CLIENT_ID;
}

// Field-by-field checks on the parsed URL, never a string match: userinfo, encoded paths,
// IPv6 literals and lookalike hosts (127.0.0.1.evil.com) all fail one of these checks.
const LOOPBACK_RULES: readonly ((url: URL) => boolean)[] = [
  (url) => url.protocol === 'http:',
  (url) => !url.username,
  (url) => !url.password,
  (url) => url.hostname === '127.0.0.1',
  (url) => url.pathname === '/callback',
  (url) => !url.search,
  (url) => !url.hash
];

function isAllowedPort(port: number): boolean {
  return port >= MIN_PORT && port <= MAX_PORT;
}

function loopbackPort(url: URL): number | null {
  if (!LOOPBACK_RULES.every((rule) => rule(url))) return null;
  const port = Number(url.port || 80);
  return isAllowedPort(port) ? port : null;
}

export function validRedirectUri(value: string | null): value is string {
  if (!value) return false;
  try {
    return loopbackPort(new URL(value)) !== null;
  } catch {
    return false;
  }
}

export function isPlatform(value: string | null): value is Platform {
  return !!value && (PLATFORMS as readonly string[]).includes(value);
}

export function validState(value: string | null): value is string {
  return !!value && value.length <= STATE_MAX;
}

export function validCodeChallenge(value: string | null, method: string | null): boolean {
  return method === 'S256' && !!value && CODE_CHALLENGE_RE.test(value);
}

export function validCodeVerifier(value: string | null): value is string {
  return !!value && CODE_VERIFIER_RE.test(value);
}

export function validCode(value: string | null): value is string {
  return !!value && CODE_RE.test(value);
}

export function validRefreshToken(value: string | null): value is string {
  return !!value && REFRESH_TOKEN_RE.test(value);
}

export function sanitizeDeviceName(value: string | null): string {
  const cleaned = (value ?? '').replace(UNSAFE_NAME_CHARS, '').trim();
  return [...cleaned].slice(0, DEVICE_NAME_MAX).join('');
}

export type AuthorizeRequest = {
  clientId: string;
  redirectUri: string;
  codeChallenge: string;
  state: string;
  deviceName: string;
  platform: Platform;
};

export type AuthorizeParseResult =
  // Neither client_id nor redirect_uri can be trusted: show a local error page, never redirect.
  | { ok: false; fatal: true }
  // redirect_uri is trustworthy: the caller redirects back with error=invalid_request&state=...
  | { ok: false; fatal: false; redirectUri: string; state: string | null }
  | { ok: true; value: AuthorizeRequest };

export function parseAuthorizeParams(params: URLSearchParams): AuthorizeParseResult {
  const clientId = params.get('client_id');
  const redirectUri = params.get('redirect_uri');
  if (!validClientId(clientId) || !validRedirectUri(redirectUri)) return { ok: false, fatal: true };

  const state = params.get('state');
  if (!validState(state)) return { ok: false, fatal: false, redirectUri, state };
  if (params.get('response_type') !== 'code') return { ok: false, fatal: false, redirectUri, state };
  if (!validCodeChallenge(params.get('code_challenge'), params.get('code_challenge_method'))) {
    return { ok: false, fatal: false, redirectUri, state };
  }
  const platform = params.get('platform');
  if (!isPlatform(platform)) return { ok: false, fatal: false, redirectUri, state };

  return {
    ok: true,
    value: {
      clientId,
      redirectUri,
      codeChallenge: params.get('code_challenge') as string,
      state,
      deviceName: sanitizeDeviceName(params.get('device_name')),
      platform
    }
  };
}

export type TokenRequest =
  | { grantType: 'authorization_code'; code: string; codeVerifier: string; redirectUri: string }
  | { grantType: 'refresh_token'; refreshToken: string };

export type TokenParseError = 'invalid_request' | 'unsupported_grant_type' | 'invalid_client';
export type TokenParseResult = { ok: true; value: TokenRequest } | { ok: false; error: TokenParseError };

function parseAuthCodeGrant(form: URLSearchParams): TokenParseResult {
  const code = form.get('code');
  const codeVerifier = form.get('code_verifier');
  const redirectUri = form.get('redirect_uri');
  if (!validCode(code)) return { ok: false, error: 'invalid_request' };
  if (!validCodeVerifier(codeVerifier)) return { ok: false, error: 'invalid_request' };
  if (!validRedirectUri(redirectUri)) return { ok: false, error: 'invalid_request' };
  return { ok: true, value: { grantType: 'authorization_code', code, codeVerifier, redirectUri } };
}

function parseRefreshGrant(form: URLSearchParams): TokenParseResult {
  const refreshToken = form.get('refresh_token');
  if (!validRefreshToken(refreshToken)) return { ok: false, error: 'invalid_request' };
  return { ok: true, value: { grantType: 'refresh_token', refreshToken } };
}

export function parseTokenRequest(form: URLSearchParams): TokenParseResult {
  if (!validClientId(form.get('client_id'))) return { ok: false, error: 'invalid_client' };

  const grantType = form.get('grant_type');
  if (grantType === 'authorization_code') return parseAuthCodeGrant(form);
  if (grantType === 'refresh_token') return parseRefreshGrant(form);
  return { ok: false, error: 'unsupported_grant_type' };
}

export function callbackUrl(redirectUri: string, params: Record<string, string>): string {
  const url = new URL(redirectUri);
  for (const [key, value] of Object.entries(params)) url.searchParams.set(key, value);
  return url.toString();
}
