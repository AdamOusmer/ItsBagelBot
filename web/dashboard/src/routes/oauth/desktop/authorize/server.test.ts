// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit();

let createCodeCalls: unknown[] = [];
let createCodeImpl: () => Promise<string> = async () => 'generated-code';
let rateDecision = { allowed: true, retryAfterSec: 0 };

const services = await import('../../../../lib/server/services');
const deviceAuthStore = await import('../../../../lib/server/device-auth-store');

mock.module('#lib/server/services', () => ({
  ...services,
  deviceCodeCreate: async (args: unknown) => {
    createCodeCalls.push(args);
    return createCodeImpl();
  }
}));
mock.module('#lib/server/device-auth-store', () => ({
  ...deviceAuthStore,
  checkConsentRate: async () => rateDecision
}));

const { load, actions } = await import('./+page.server');

type Thrown = { status: number; location?: string; body?: { message?: string } };

async function runLoad(url: string, session: unknown): Promise<Thrown | { data: unknown }> {
  try {
    const data = await load({ url: new URL(url), locals: { session } } as never);
    return { data };
  } catch (e) {
    return e as Thrown;
  }
}

const GOOD_VERIFIER = 'dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXkabc';
const GOOD_CHALLENGE = 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM';
const GOOD_QUERY =
  'response_type=code&client_id=itsbagelbot-desktop&redirect_uri=http%3A%2F%2F127.0.0.1%3A51123%2Fcallback' +
  `&code_challenge=${GOOD_CHALLENGE}&code_challenge_method=S256&state=xyz&device_name=My+Laptop&platform=macos`;
const AUTHORIZE_URL = `https://dashboard.itsbagelbot.com/oauth/desktop/authorize?${GOOD_QUERY}`;

const SESSION = { user_id: '42', login: 'streamer', display_name: 'Streamer' };

beforeEach(() => {
  createCodeCalls = [];
  createCodeImpl = async () => 'generated-code';
  rateDecision = { allowed: true, retryAfterSec: 0 };
});

describe('GET /oauth/desktop/authorize (load)', () => {
  test('a bad client_id never redirects: it answers a local error page', async () => {
    const url = AUTHORIZE_URL.replace('client_id=itsbagelbot-desktop', 'client_id=someone-else');
    const result = await runLoad(url, SESSION);
    expect((result as Thrown).status).toBe(400);
    expect((result as Thrown).location).toBeUndefined();
  });

  test('a bad redirect_uri never redirects: it answers a local error page', async () => {
    const url = AUTHORIZE_URL.replace(encodeURIComponent('http://127.0.0.1:51123/callback'), encodeURIComponent('https://evil.example/callback'));
    const result = await runLoad(url, SESSION);
    expect((result as Thrown).status).toBe(400);
  });

  test('a bad platform redirects back to the app with invalid_request, state preserved', async () => {
    const url = AUTHORIZE_URL.replace('platform=macos', 'platform=amiga');
    const result = await runLoad(url, SESSION);
    expect((result as Thrown).status).toBe(302);
    expect((result as Thrown).location).toBe('http://127.0.0.1:51123/callback?error=invalid_request&state=xyz');
  });

  test('not signed in redirects to login with a safe next path back to this request', async () => {
    const result = await runLoad(AUTHORIZE_URL, null);
    expect((result as Thrown).status).toBe(302);
    const location = (result as Thrown).location as string;
    expect(location.startsWith('/login?next=')).toBe(true);
    const next = decodeURIComponent(location.slice('/login?next='.length));
    expect(next).toBe(`/oauth/desktop/authorize?${GOOD_QUERY}`);
  });

  test('an impersonated session gets a refusal, not a redirect', async () => {
    const result = await runLoad(AUTHORIZE_URL, { ...SESSION, impersonator_id: 'staff-1' });
    expect('data' in result).toBe(true);
    expect((result as { data: { refusal: string } }).data.refusal).toBe('impersonator');
  });

  test('a delegate session gets a refusal, not a redirect', async () => {
    const result = await runLoad(AUTHORIZE_URL, { ...SESSION, delegate_of: 'owner-1' });
    expect('data' in result).toBe(true);
    expect((result as { data: { refusal: string } }).data.refusal).toBe('delegate');
  });

  test('a normal signed-in session gets the parsed request with no refusal', async () => {
    const result = await runLoad(AUTHORIZE_URL, SESSION);
    const data = (result as { data: { refusal: unknown; request: { deviceName: string; platform: string } } }).data;
    expect(data.refusal).toBeNull();
    expect(data.request.deviceName).toBe('My Laptop');
    expect(data.request.platform).toBe('macos');
  });
});

function formFrom(fields: Record<string, string>): FormData {
  const form = new FormData();
  for (const [key, value] of Object.entries(fields)) form.set(key, value);
  return form;
}

const GOOD_FIELDS = {
  response_type: 'code',
  client_id: 'itsbagelbot-desktop',
  redirect_uri: 'http://127.0.0.1:51123/callback',
  code_challenge: GOOD_CHALLENGE,
  code_challenge_method: 'S256',
  state: 'xyz',
  device_name: 'My Laptop',
  platform: 'macos'
};

function postEvent(session: unknown, fields: Record<string, string>) {
  return {
    request: { formData: async () => formFrom(fields) },
    locals: { session },
    getClientAddress: () => '203.0.113.9'
  } as never;
}

describe('POST /oauth/desktop/authorize (approve)', () => {
  test('rate limited answers 429 before touching the session or Users', async () => {
    rateDecision = { allowed: false, retryAfterSec: 5 };
    const result = await actions.approve(postEvent(SESSION, GOOD_FIELDS));
    expect((result as { status: number }).status).toBe(429);
    expect(createCodeCalls.length).toBe(0);
  });

  test('not signed in (session lost mid-flow) answers 401, not a redirect', async () => {
    const result = await actions.approve(postEvent(null, GOOD_FIELDS));
    expect((result as { status: number }).status).toBe(401);
  });

  test('an impersonated session is refused with 403', async () => {
    const result = await actions.approve(postEvent({ ...SESSION, impersonator_id: 'staff-1' }, GOOD_FIELDS));
    expect((result as { status: number }).status).toBe(403);
  });

  test('a delegate session is refused with 403', async () => {
    const result = await actions.approve(postEvent({ ...SESSION, delegate_of: 'owner-1' }, GOOD_FIELDS));
    expect((result as { status: number }).status).toBe(403);
  });

  test('tampered redirect_uri on resubmit is fatal: a local error, not a redirect', async () => {
    try {
      await actions.approve(postEvent(SESSION, { ...GOOD_FIELDS, redirect_uri: 'https://evil.example/callback' }));
      throw new Error('expected a throw');
    } catch (e) {
      expect((e as { status: number }).status).toBe(400);
    }
  });

  test('approval calls Users with the resubmitted fields and redirects with code and state', async () => {
    const result = (await actions.approve(postEvent(SESSION, GOOD_FIELDS))) as {
      ok: boolean;
      action: string;
      target: string;
    };
    expect(result).toEqual({ ok: true, action: 'approved', target: 'http://127.0.0.1:51123/callback?code=generated-code&state=xyz' });
    expect(createCodeCalls).toEqual([
      {
        userId: '42',
        codeChallenge: GOOD_CHALLENGE,
        redirectUri: 'http://127.0.0.1:51123/callback',
        deviceName: 'My Laptop',
        platform: 'macos'
      }
    ]);
  });

  test('a Users RPC failure answers 502 with no partial redirect state', async () => {
    createCodeImpl = async () => {
      throw new Error('nats timeout');
    };
    const result = await actions.approve(postEvent(SESSION, GOOD_FIELDS));
    expect((result as { status: number }).status).toBe(502);
  });
});

describe('POST /oauth/desktop/authorize (cancel)', () => {
  test('cancel redirects with error=access_denied and the original state, without calling Users', async () => {
    const result = (await actions.cancel(postEvent(SESSION, GOOD_FIELDS))) as { ok: boolean; action: string; target: string };
    expect(result).toEqual({ ok: true, action: 'cancelled', target: 'http://127.0.0.1:51123/callback?error=access_denied&state=xyz' });
    expect(createCodeCalls.length).toBe(0);
  });
});
