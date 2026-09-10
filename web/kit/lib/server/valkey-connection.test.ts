// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, test } from 'bun:test';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  VALKEY_TLS_DATA_PORT,
  VALKEY_TLS_SENTINEL_PORT,
  valkeyEndpoint,
  valkeySentinelNAT,
  valkeyTLSOptions
} from './valkey-connection';

describe('Valkey native TLS connection policy', () => {
  test('moves data and Sentinel endpoints onto their TLS listeners', () => {
    expect(valkeyEndpoint('valkey.valkey.svc.cluster.local:6379', true, VALKEY_TLS_DATA_PORT)).toEqual({
      host: 'valkey.valkey.svc.cluster.local',
      port: 6380
    });
    expect(
      valkeyEndpoint('valkey.valkey.svc.cluster.local:26379', true, VALKEY_TLS_SENTINEL_PORT)
    ).toEqual({ host: 'valkey.valkey.svc.cluster.local', port: 26380 });
  });

  test('verifies the fleet CA and remaps stale Sentinel data ports', () => {
    expect(valkeyTLSOptions({ addr: 'valkey:6379', tlsCa: 'pem' })).toMatchObject({
      ca: 'pem',
      servername: 'valkey.valkey.svc.cluster.local',
      minVersion: 'TLSv1.2'
    });
    expect(valkeySentinelNAT('100.99.41.21:6379')).toEqual({ host: '100.99.41.21', port: 6380 });
    expect(valkeySentinelNAT('100.99.41.21:6380')).toBeNull();
  });

  test('mTLS cert/key stay unset without both env-mounted paths (both-or-neither)', () => {
    expect(valkeyTLSOptions({ addr: 'valkey:6379', tlsCa: 'pem' })).not.toHaveProperty('cert');
    expect(
      valkeyTLSOptions({ addr: 'valkey:6379', tlsCa: 'pem', tlsClientCertFile: '/only/cert' })
    ).not.toHaveProperty('cert');
  });

  // Refs #560: the client cert must be re-read from disk on every reconnect,
  // not baked in once at options-build time, or a cert-manager rotation at
  // day 75 leaves the process presenting an expired cert at day 90. iovalkey's
  // connectors re-copy the `tls` options object into the socket options on
  // every connect() call, and Object.assign invokes property getters at copy
  // time -- so re-reading `options.cert`/`options.key` here stands in for
  // that per-reconnect re-copy and proves the getter, not a cached value, is
  // what iovalkey will observe on the next handshake.
  test('mTLS cert/key are re-read from disk on every access, not cached from construction', () => {
    const dir = mkdtempSync(join(tmpdir(), 'valkey-mtls-'));
    const certFile = join(dir, 'tls.crt');
    const keyFile = join(dir, 'tls.key');
    writeFileSync(certFile, 'cert-v1');
    writeFileSync(keyFile, 'key-v1');

    const options = valkeyTLSOptions({
      addr: 'valkey:6379',
      tlsCa: 'pem',
      tlsClientCertFile: certFile,
      tlsClientKeyFile: keyFile
    });

    expect(options?.cert?.toString()).toBe('cert-v1');
    expect(options?.key?.toString()).toBe('key-v1');

    // cert-manager's renewal: same path, rotated content.
    writeFileSync(certFile, 'cert-v2');
    writeFileSync(keyFile, 'key-v2');

    expect(options?.cert?.toString()).toBe('cert-v2');
    expect(options?.key?.toString()).toBe('key-v2');
  });
});
