// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { beforeEach, describe, expect, mock, test } from 'bun:test';

const privateEnv: Record<string, string | undefined> = {};
const query = mock(async () => [[], []]);
const end = mock(async () => {});
const createConnection = mock(async () => ({ query, end }));

mock.module('$env/dynamic/private', () => ({ env: privateEnv }));
mock.module('mysql2/promise', () => ({ default: { createConnection } }));

const { revokeCredential } = await import('../src/lib/server/secrets');

beforeEach(() => {
  for (const key of Object.keys(privateEnv)) delete privateEnv[key];
  Object.assign(privateEnv, {
    DB_ADMIN_HOST: '10.0.0.4',
    DB_ADMIN_USER: 'db_admin',
    DB_ADMIN_PASS: 'secret'
  });
  createConnection.mockClear();
  query.mockClear();
  end.mockClear();
});

describe('admin database TLS', () => {
  test('fails closed before connecting when no CA is configured', async () => {
    await expect(
      revokeCredential('notifications', { dbUser: 'notifications_svc_old' })
    ).rejects.toThrow('DB admin CA certificate is not configured');

    expect(createConnection).not.toHaveBeenCalled();
  });

  test('uses the configured HeatWave CA', async () => {
    privateEnv.DB_CA_CERT = 'heatwave-ca-pem';

    await revokeCredential('notifications', { dbUser: 'notifications_svc_old' });

    expect(createConnection).toHaveBeenCalledTimes(1);
    expect(createConnection.mock.calls[0]?.[0]).toMatchObject({
      host: '10.0.0.4',
      user: 'db_admin',
      ssl: { ca: 'heatwave-ca-pem', rejectUnauthorized: true, minVersion: 'TLSv1.2' }
    });
    expect(query).toHaveBeenCalledTimes(2);
    expect(end).toHaveBeenCalledTimes(1);
  });
});
