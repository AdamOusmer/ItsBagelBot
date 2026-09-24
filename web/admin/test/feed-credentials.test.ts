// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import type { Authenticator } from '@nats-io/transport-node';
import { feedOptions } from '../src/lib/server/feed';

// Throwaway user nkey seed generated for this test only; not a live credential.
const SEED = 'SUAIVAGIKFP625PEBOON4MYTSXVJKY3TIGDMTGNYW47VW45MNW4EWO32KQ';

const CREDENTIAL_ENV_KEYS = ['NATS_USER', 'NATS_PASSWORD', 'NATS_JWT', 'NATS_NKEY_SEED'];
const saved: Record<string, string | undefined> = {};

beforeEach(() => {
  for (const key of CREDENTIAL_ENV_KEYS) {
    saved[key] = process.env[key];
    delete process.env[key];
  }
});

afterEach(() => {
  for (const key of CREDENTIAL_ENV_KEYS) {
    if (saved[key] === undefined) delete process.env[key];
    else process.env[key] = saved[key];
  }
});

describe('feed connection options', () => {
  test('sends only the JWT, even with leftover password vars', () => {
    process.env.NATS_USER = 'u';
    process.env.NATS_PASSWORD = 'p';
    process.env.NATS_JWT = 'bus-jwt';
    process.env.NATS_NKEY_SEED = SEED;

    const opts = feedOptions();
    expect(opts.user).toBeUndefined();
    expect(opts.pass).toBeUndefined();
    const authenticator = opts.authenticator as Authenticator[];
    expect(authenticator).toHaveLength(1);
    expect((authenticator[0]('nonce') as { jwt: string }).jwt).toBe('bus-jwt');
  });

  test('no JWT vars means no credentials at all', () => {
    expect(feedOptions().authenticator).toBeUndefined();
  });
});
