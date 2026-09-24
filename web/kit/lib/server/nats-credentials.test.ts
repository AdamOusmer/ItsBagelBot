// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';

mock.module('newrelic', () => ({
  default: {
    startSegment: (_name: string, _record: boolean, run: () => unknown) => run(),
    recordMetric: () => {}
  }
}));
const { credentialAuth, options } = await import('./nats');

// Throwaway user nkey seed generated for this test only; not a live credential.
const SEED = 'SUAIVAGIKFP625PEBOON4MYTSXVJKY3TIGDMTGNYW47VW45MNW4EWO32KQ';

const CREDENTIAL_ENV_KEYS = [
  'NATS_USER',
  'NATS_PASSWORD',
  'NATS_JWT',
  'NATS_NKEY_SEED',
  'NATS_RPC_JWT',
  'NATS_RPC_NKEY_SEED'
];
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

describe('credentialAuth', () => {
  test('a JWT and seed build a single JWT authenticator', () => {
    const auth = credentialAuth('a-jwt', SEED);
    expect(auth.authenticator).toHaveLength(1);
    const creds = auth.authenticator![0]('nonce') as { jwt: string; sig: string };
    expect(creds.jwt).toBe('a-jwt');
    expect(creds.sig.length).toBeGreaterThan(0);
  });

  test('either var alone yields an empty options patch', () => {
    expect(credentialAuth('a-jwt', undefined)).toEqual({});
    expect(credentialAuth(undefined, SEED)).toEqual({});
  });
});

describe('options() credential wiring', () => {
  test('leftover password vars are never sent', () => {
    process.env.NATS_USER = 'u';
    process.env.NATS_PASSWORD = 'p';
    process.env.NATS_JWT = 'bus-jwt';
    process.env.NATS_NKEY_SEED = SEED;

    const opts = options('bus');
    expect(opts.user).toBeUndefined();
    expect(opts.pass).toBeUndefined();
    expect(opts.authenticator).toHaveLength(1);
  });

  test('RPC falls back to the bus JWT vars', () => {
    process.env.NATS_JWT = 'bus-jwt';
    process.env.NATS_NKEY_SEED = SEED;

    const opts = options('rpc');
    expect(opts.authenticator).toHaveLength(1);
    const creds = opts.authenticator![0]('nonce') as { jwt: string };
    expect(creds.jwt).toBe('bus-jwt');
  });

  test('RPC-specific JWT vars win over the bus fallback', () => {
    process.env.NATS_JWT = 'bus-jwt';
    process.env.NATS_NKEY_SEED = SEED;
    process.env.NATS_RPC_JWT = 'rpc-jwt';
    process.env.NATS_RPC_NKEY_SEED = SEED;

    const creds = options('rpc').authenticator![0]('nonce') as { jwt: string };
    expect(creds.jwt).toBe('rpc-jwt');
  });

  test('the bus role never reads the RPC-specific JWT vars', () => {
    process.env.NATS_RPC_JWT = 'rpc-jwt';
    process.env.NATS_RPC_NKEY_SEED = SEED;

    expect(options('bus').authenticator).toBeUndefined();
  });
});
