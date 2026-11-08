// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  DESKTOP_CLIENT_ID,
  callbackUrl,
  parseAuthorizeParams,
  sanitizeDeviceName,
  validClientId,
  validCodeChallenge,
  validDPoPThumbprint,
  validRedirectUri,
  validState
} from './device-auth-validate';

const GOOD_CHALLENGE = 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM';
const GOOD_JKT = 'a'.repeat(42) + 'A';

function goodParams(overrides: Record<string, string | undefined> = {}): URLSearchParams {
  const base: Record<string, string> = {
    response_type: 'code',
    client_id: DESKTOP_CLIENT_ID,
    redirect_uri: 'http://127.0.0.1:51123/callback',
    code_challenge: GOOD_CHALLENGE,
    code_challenge_method: 'S256',
    dpop_jkt: GOOD_JKT,
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
});

describe('DPoP authorization binding', () => {
  test('accepts canonical SHA-256 thumbprints only', () => {
    expect(validDPoPThumbprint(GOOD_JKT)).toBe(true);
    for (const value of [null, '', 'a'.repeat(42), 'a'.repeat(44), 'a'.repeat(42) + 'B', GOOD_JKT + '=', '+' + GOOD_JKT.slice(1)]) {
      expect(validDPoPThumbprint(value)).toBe(false);
    }
  });

  test('missing and malformed thumbprints cannot create unbound authorization codes', () => {
    for (const dpop_jkt of [undefined, '', 'a'.repeat(42) + 'B']) {
      expect(parseAuthorizeParams(goodParams({ dpop_jkt }))).toEqual({
        ok: false, fatal: false, redirectUri: 'http://127.0.0.1:51123/callback', state: 'xyz'
      });
    }
  });

  test('duplicate thumbprints are refused instead of choosing one key', () => {
    const params = goodParams();
    params.append('dpop_jkt', 'b'.repeat(42) + 'A');
    expect(parseAuthorizeParams(params).ok).toBe(false);
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
      expect(result.value.dpopJkt).toBe(GOOD_JKT);
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
