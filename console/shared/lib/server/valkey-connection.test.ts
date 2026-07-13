// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, test } from 'bun:test';
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
});
