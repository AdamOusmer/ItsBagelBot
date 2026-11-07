// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  DESKTOP_CLIENT_ID,
  callbackUrl,
  parseAuthorizeParams,
  parseTokenRequest,
  sanitizeDeviceName,
  validClientId,
  validCode,
  validCodeChallenge,
  validCodeVerifier,
  validRedirectUri,
  validRefreshToken,
  validState
} from './device-auth-validate';

const GOOD_CHALLENGE = 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM';
const GOOD_VERIFIER = 'dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk'.padEnd(43, 'x');

function goodParams(overrides: Record<string, string | undefined> = {}): URLSearchParams {
  const base: Record<string, string> = {
    response_type: 'code',
    client_id: DESKTOP_CLIENT_ID,
    redirect_uri: 'http://127.0.0.1:51123/callback',
    code_challenge: GOOD_CHALLENGE,
    code_challenge_method: 'S256',
    state: 'xyz',
    device_name: "Adam's MacBook",
    platform: 'macos'
  };
  const merged = { ...base, ...overrides };
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(merged)) {
    if (value !== undefined) params.set(key, value);
  }
  return params;
}

describe('redirect_uri validation', () => {
  const BAD: Record<string, string> = {
    localhost: 'http://localhost:51123/callback',
    'lookalike host': 'http://127.0.0.1.evil.com:51123/callback',
    'userinfo prefix': 'http://evil@127.0.0.1:51123/callback',
    'userinfo with password': 'http://127.0.0.1:x@127.0.0.1:51123/callback',
    'ipv6 loopback': 'http://[::1]:51123/callback',
    'https scheme': 'https://127.0.0.1:51123/callback',
    'extra query': 'http://127.0.0.1:51123/callback?x=1',
    fragment: 'http://127.0.0.1:51123/callback#frag',
    'wrong path': 'http://127.0.0.1:51123/other',
    'extra path segment': 'http://127.0.0.1:51123/callback/extra',
    'port too low': 'http://127.0.0.1:80/callback',
    'port at boundary below range': 'http://127.0.0.1:1023/callback',
    'port too high': 'http://127.0.0.1:65536/callback',
    'default port (none given)': 'http://127.0.0.1/callback',
    'not a url': 'not-a-url',
    empty: ''
  };

  for (const [name, uri] of Object.entries(BAD)) {
    test(`rejects ${name}`, () => {
      expect(validRedirectUri(uri)).toBe(false);
    });
  }

  test('accepts the exact loopback shape at both port boundaries', () => {
    expect(validRedirectUri('http://127.0.0.1:1024/callback')).toBe(true);
    expect(validRedirectUri('http://127.0.0.1:65535/callback')).toBe(true);
  });

  test('rejects null', () => {
    expect(validRedirectUri(null)).toBe(false);
  });
});

describe('client_id validation', () => {
  test('accepts only the exact literal', () => {
    expect(validClientId(DESKTOP_CLIENT_ID)).toBe(true);
    expect(validClientId('itsbagelbot-desktop ')).toBe(false);
    expect(validClientId('ItsBagelBot-Desktop')).toBe(false);
    expect(validClientId(null)).toBe(false);
  });
});

describe('PKCE validation', () => {
  test('S256 with a 43-character base64url challenge passes', () => {
    expect(validCodeChallenge(GOOD_CHALLENGE, 'S256')).toBe(true);
  });
  test('plain method is refused even with a well-formed challenge', () => {
    expect(validCodeChallenge(GOOD_CHALLENGE, 'plain')).toBe(false);
  });
  test('wrong-length or non-base64url challenges are refused', () => {
    expect(validCodeChallenge(GOOD_CHALLENGE.slice(0, 42), 'S256')).toBe(false);
    expect(validCodeChallenge(`${GOOD_CHALLENGE.slice(0, 42)}=`, 'S256')).toBe(false);
  });
  test('verifier accepts the 43-128 character unreserved charset', () => {
    expect(validCodeVerifier(GOOD_VERIFIER)).toBe(true);
    expect(validCodeVerifier('a'.repeat(42))).toBe(false);
    expect(validCodeVerifier('a'.repeat(129))).toBe(false);
    expect(validCodeVerifier('has a space'.padEnd(43, 'x'))).toBe(false);
  });
});

describe('state validation', () => {
  test('required and capped at 512 characters', () => {
    expect(validState('a')).toBe(true);
    expect(validState('a'.repeat(512))).toBe(true);
    expect(validState('a'.repeat(513))).toBe(false);
    expect(validState('')).toBe(false);
    expect(validState(null)).toBe(false);
  });
});

describe('device_name sanitization', () => {
  test('strips control characters and truncates at 64', () => {
    expect(sanitizeDeviceName('A\u0000B\u001fC\u007fD')).toBe('ABCD');
    expect(sanitizeDeviceName('x'.repeat(100))).toHaveLength(64);
    expect(sanitizeDeviceName(null)).toBe('');
  });

  test('strips bidi and other format characters', () => {
    expect(sanitizeDeviceName('Mac‮evil‬')).toBe('Macevil');
    expect(sanitizeDeviceName('⁦Studio⁩ PC')).toBe('Studio PC');
    expect(sanitizeDeviceName('Zero​width')).toBe('Zerowidth');
    expect(sanitizeDeviceName('Café 配信')).toBe('Café 配信');
  });

  test('truncates by character, matching Users, and never splits a surrogate pair', () => {
    expect(sanitizeDeviceName('😀'.repeat(70))).toBe('😀'.repeat(64));
  });
});

describe('parseAuthorizeParams', () => {
  test('a good request parses with the sanitized device name intact', () => {
    const result = parseAuthorizeParams(goodParams());
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.value.platform).toBe('macos');
      expect(result.value.deviceName).toBe("Adam's MacBook");
    }
  });

  test('a bad client_id is fatal: never a redirect target', () => {
    const result = parseAuthorizeParams(goodParams({ client_id: 'someone-else' }));
    expect(result).toEqual({ ok: false, fatal: true });
  });

  test('a bad redirect_uri is fatal: never a redirect target', () => {
    const result = parseAuthorizeParams(goodParams({ redirect_uri: 'https://evil.example/callback' }));
    expect(result).toEqual({ ok: false, fatal: true });
  });

  test('an unknown platform is not fatal: redirect_uri is trustworthy, state passes through', () => {
    const result = parseAuthorizeParams(goodParams({ platform: 'amiga', state: 'carry-me' }));
    expect(result.ok).toBe(false);
    if (!result.ok && !result.fatal) {
      expect(result.redirectUri).toBe('http://127.0.0.1:51123/callback');
      expect(result.state).toBe('carry-me');
    } else {
      throw new Error('expected a non-fatal result');
    }
  });

  test('plain PKCE method is not fatal', () => {
    const result = parseAuthorizeParams(goodParams({ code_challenge_method: 'plain' }));
    expect(result.ok).toBe(false);
    if (result.ok) throw new Error('expected rejection');
    expect(result.fatal).toBe(false);
  });

  test('a missing state is not fatal and carries no state to redirect with', () => {
    const result = parseAuthorizeParams(goodParams({ state: undefined }));
    expect(result.ok).toBe(false);
    if (!result.ok && !result.fatal) expect(result.state).toBeNull();
  });
});

describe('parseTokenRequest', () => {
  function form(overrides: Record<string, string | undefined>): URLSearchParams {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(overrides)) {
      if (value !== undefined) params.set(key, value);
    }
    return params;
  }

  test('authorization_code grant parses with all three required fields', () => {
    const result = parseTokenRequest(
      form({
        grant_type: 'authorization_code',
        client_id: DESKTOP_CLIENT_ID,
        code: 'abc123_-',
        code_verifier: GOOD_VERIFIER,
        redirect_uri: 'http://127.0.0.1:51123/callback'
      })
    );
    expect(result).toEqual({
      ok: true,
      value: {
        grantType: 'authorization_code',
        code: 'abc123_-',
        codeVerifier: GOOD_VERIFIER,
        redirectUri: 'http://127.0.0.1:51123/callback'
      }
    });
  });

  test('refresh_token grant requires the bbr_ prefix', () => {
    const good = form({ grant_type: 'refresh_token', client_id: DESKTOP_CLIENT_ID, refresh_token: 'bbr_abc123' });
    expect(parseTokenRequest(good)).toEqual({
      ok: true,
      value: { grantType: 'refresh_token', refreshToken: 'bbr_abc123' }
    });

    const missingPrefix = form({ grant_type: 'refresh_token', client_id: DESKTOP_CLIENT_ID, refresh_token: 'abc123' });
    expect(parseTokenRequest(missingPrefix)).toEqual({ ok: false, error: 'invalid_request' });
  });

  test('wrong client_id refuses before the grant type is even read', () => {
    expect(parseTokenRequest(form({ grant_type: 'authorization_code', client_id: 'other' }))).toEqual({
      ok: false,
      error: 'invalid_client'
    });
  });

  test('an unsupported grant type is refused', () => {
    expect(parseTokenRequest(form({ grant_type: 'password', client_id: DESKTOP_CLIENT_ID }))).toEqual({
      ok: false,
      error: 'unsupported_grant_type'
    });
  });

  test('authorization_code grant with a malformed verifier is refused', () => {
    const result = parseTokenRequest(
      form({
        grant_type: 'authorization_code',
        client_id: DESKTOP_CLIENT_ID,
        code: 'abc123',
        code_verifier: 'too-short',
        redirect_uri: 'http://127.0.0.1:51123/callback'
      })
    );
    expect(result).toEqual({ ok: false, error: 'invalid_request' });
  });

  test('authorization_code grant with a non-loopback redirect_uri is refused', () => {
    const result = parseTokenRequest(
      form({
        grant_type: 'authorization_code',
        client_id: DESKTOP_CLIENT_ID,
        code: 'abc123',
        code_verifier: GOOD_VERIFIER,
        redirect_uri: 'https://evil.example/callback'
      })
    );
    expect(result).toEqual({ ok: false, error: 'invalid_request' });
  });
});

describe('validCode / validRefreshToken', () => {
  test('code accepts the base64url alphabet only', () => {
    expect(validCode('abc-_123')).toBe(true);
    expect(validCode('has a space')).toBe(false);
    expect(validCode(null)).toBe(false);
  });
  test('refresh token requires the bbr_ prefix', () => {
    expect(validRefreshToken('bbr_abc')).toBe(true);
    expect(validRefreshToken('abc')).toBe(false);
  });
});

describe('callbackUrl', () => {
  test('sets query params on the loopback redirect without disturbing the base', () => {
    const url = callbackUrl('http://127.0.0.1:51123/callback', { code: 'abc', state: 'xyz' });
    expect(url).toBe('http://127.0.0.1:51123/callback?code=abc&state=xyz');
  });
  test('builds the cancel shape', () => {
    const url = callbackUrl('http://127.0.0.1:51123/callback', { error: 'access_denied', state: 'xyz' });
    expect(url).toBe('http://127.0.0.1:51123/callback?error=access_denied&state=xyz');
  });
});
