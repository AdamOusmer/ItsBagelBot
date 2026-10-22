// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';

mock.module('newrelic', () => ({
  default: {
    startSegment: (_name: string, _record: boolean, run: () => unknown) => run(),
    recordMetric: () => {}
  }
}));
const { RpcError, credentialAuth, hubJetStreamOptions, localLeafReady, options, rpcRefusal } = await import('./nats');

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

const jwtOf = (opts: ReturnType<typeof options>) =>
  (opts.authenticator![0]('nonce') as { jwt: string }).jwt;

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
    Object.assign(process.env, { NATS_USER: 'u', NATS_PASSWORD: 'p', NATS_JWT: 'bus-jwt', NATS_NKEY_SEED: SEED });

    const opts = options('bus');
    expect([opts.user, opts.pass]).toEqual([undefined, undefined]);
    expect(opts.authenticator).toHaveLength(1);
  });

  test('RPC falls back to the bus JWT vars', () => {
    Object.assign(process.env, { NATS_JWT: 'bus-jwt', NATS_NKEY_SEED: SEED });

    expect(jwtOf(options('rpc'))).toBe('bus-jwt');
  });

  test('RPC-specific JWT vars win over the bus fallback', () => {
    Object.assign(process.env, {
      NATS_JWT: 'bus-jwt',
      NATS_NKEY_SEED: SEED,
      NATS_RPC_JWT: 'rpc-jwt',
      NATS_RPC_NKEY_SEED: SEED
    });

    expect(jwtOf(options('rpc'))).toBe('rpc-jwt');
  });

  test('the bus role never reads the RPC-specific JWT vars', () => {
    Object.assign(process.env, { NATS_RPC_JWT: 'rpc-jwt', NATS_RPC_NKEY_SEED: SEED });

    expect(options('bus').authenticator).toBeUndefined();
  });
});

describe('options() server pool order', () => {
  test.each([
    ['rpc', true, true],
    ['bus', false, true]
  ] as const)('%s plane', (role, noRandomize, ignoreClusterUpdates) => {
    const opts = options(role);
    expect(opts.noRandomize).toBe(noRandomize);
    expect(opts.ignoreClusterUpdates).toBe(ignoreClusterUpdates);
  });
});

describe('local leaf failback probe', () => {
  const server = Bun.serve({
    port: 0,
    fetch(request) {
      const path = new URL(request.url).pathname;
      return path === '/healthz' ? new Response('ok') : new Response('missing', { status: 404 });
    }
  });

  afterAll(() => server.stop(true));

  test('accepts the monitor health endpoint', async () => {
    expect(await localLeafReady(`${server.url}healthz`, 500)).toBe(true);
  });

  test('rejects non-200 and unreachable endpoints', async () => {
    expect(await localLeafReady(`${server.url}missing`, 500)).toBe(false);
    expect(await localLeafReady('http://127.0.0.1:1/healthz', 25)).toBe(false);
  });
});

describe('localLeafReady over TLS', () => {
  const dir = mkdtempSync(join(tmpdir(), 'leaf-health-'));
  const file = (name: string) => join(dir, name);
  const openssl = (...args: string[]) => {
    const run = Bun.spawnSync(['openssl', ...args]);
    if (run.exitCode !== 0) throw new Error(run.stderr.toString());
  };
  const ec = ['-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:prime256v1', '-nodes'];
  writeFileSync(file('leaf.ext'), 'subjectAltName=DNS:localhost');
  openssl('req', '-x509', ...ec, '-days', '1', '-subj', '/CN=fleet-ca', '-keyout', file('ca.key'), '-out', file('ca.pem'));
  openssl('req', '-new', ...ec, '-subj', '/CN=leaf', '-keyout', file('leaf.key'), '-out', file('leaf.csr'));
  openssl('x509', '-req', '-in', file('leaf.csr'), '-CA', file('ca.pem'), '-CAkey', file('ca.key'),
    '-set_serial', '1', '-days', '1', '-extfile', file('leaf.ext'), '-out', file('leaf.pem'));
  openssl('req', '-x509', ...ec, '-days', '1', '-subj', '/CN=other-ca', '-keyout', file('other.key'), '-out', file('other.pem'));

  const server = Bun.serve({
    port: 0,
    hostname: '127.0.0.1',
    tls: { cert: Bun.file(file('leaf.pem')), key: Bun.file(file('leaf.key')) },
    fetch: () => new Response('ok')
  });
  const fleetCA = readFileSync(file('ca.pem'), 'utf8');
  const otherCA = readFileSync(file('other.pem'), 'utf8');

  afterAll(() => {
    server.stop(true);
    rmSync(dir, { recursive: true, force: true });
  });

  test('accepts a leaf signed by the fleet CA', async () => {
    expect(await localLeafReady(`https://localhost:${server.port}/healthz`, 2000, fleetCA)).toBe(true);
  });

  test('rejects a leaf outside the fleet CA', async () => {
    expect(await localLeafReady(`https://localhost:${server.port}/healthz`, 2000, otherCA)).toBe(false);
  });

  test('rejects a host name missing from the leaf certificate', async () => {
    expect(await localLeafReady(`https://127.0.0.1:${server.port}/healthz`, 2000, fleetCA)).toBe(false);
  });
});

describe('direct-hub JetStream options', () => {
  test('uses the API prefix authorized on the hub account', () => {
    expect(hubJetStreamOptions()).toEqual({ apiPrefix: '$JS.API', checkAPI: false });
  });

  test('returns a fresh object for client normalization', () => {
    expect(hubJetStreamOptions()).not.toBe(hubJetStreamOptions());
  });
});

describe('rpcRefusal', () => {
  test('a reply without an error is not a refusal', () => {
    expect(rpcRefusal({ ok: true })).toBeNull();
    expect(rpcRefusal(null)).toBeNull();
    expect(rpcRefusal({ error: '' })).toBeNull();
  });

  test('a refusal keeps the message and the shared code', () => {
    const r = rpcRefusal({ error: 'no such board', code: 'not_found' });
    expect(r).toBeInstanceOf(RpcError);
    expect(r?.message).toBe('no such board');
    expect(r?.code).toBe('not_found');
  });

  test('a code outside the shared vocabulary is unclassified, never a success', () => {
    const r = rpcRefusal({ error: 'this Discord server is not connected to your Twitch channel', code: 'not_bound' });
    expect(r).toBeInstanceOf(RpcError);
    expect(r?.code).toBe('');
  });
});
