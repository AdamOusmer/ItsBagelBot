// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { afterEach, beforeEach, describe, expect, mock, spyOn, test } from 'bun:test';

const privateEnv: Record<string, string | undefined> = {};
const query = mock(async (_sql: string, _params?: unknown[]) => [[], []]);
const end = mock(async () => {});
const createConnection = mock(async (_options: unknown) => ({ query, end }));

mock.module('$env/dynamic/private', () => ({ env: privateEnv }));
mock.module('mysql2/promise', () => ({ default: { createConnection } }));

const { revokeCredential, rotateCredential, setCredential } = await import(
  '../src/lib/server/secrets'
);

type DopplerSecrets = Record<string, string>;

/** Doppler config as the service pod sees it: a client key pair unless a test blanks it. */
function dopplerConfig(overrides: DopplerSecrets = {}): DopplerSecrets {
  return { DB_USER: 'users_svc_old', DB_CLIENT_CERT: 'cert-pem', DB_CLIENT_KEY: 'key-pem', ...overrides };
}

let dopplerSecrets: DopplerSecrets = dopplerConfig();
let dopplerWriteOk = true;
let fetchSpy: ReturnType<typeof spyOn>;

function dopplerFetch(input: string | URL | Request, init?: RequestInit): Promise<Response> {
  const url = String(input instanceof Request ? input.url : input);
  if (!url.startsWith('https://api.doppler.com/v3/configs/config/secrets')) {
    throw new Error(`unexpected fetch ${url}`);
  }
  if (init?.method === 'POST') return Promise.resolve(new Response('{}', { status: dopplerWriteOk ? 200 : 500 }));
  const secrets = Object.fromEntries(
    Object.entries(dopplerSecrets).map(([key, value]) => [key, { raw: value, computed: value }])
  );
  return Promise.resolve(Response.json({ secrets }));
}

function sql(): string[] {
  return query.mock.calls.map((call: unknown[]) => String(call[0]));
}

function dopplerWrites(): Record<string, string>[] {
  return fetchSpy.mock.calls
    .filter((call: unknown[]) => (call[1] as RequestInit | undefined)?.method === 'POST')
    .map((call: unknown[]) => JSON.parse(String((call[1] as RequestInit).body)).secrets);
}

beforeEach(() => {
  for (const key of Object.keys(privateEnv)) delete privateEnv[key];
  Object.assign(privateEnv, {
    DB_ADMIN_HOST: '10.0.0.4',
    DB_ADMIN_USER: 'db_admin',
    DB_ADMIN_PASS: 'secret',
    DB_CA_CERT: 'heatwave-ca-pem',
    DOPPLER_TOKEN_USERS: 'dp.st.users',
    DOPPLER_TOKEN_DISCORD_DATA: 'dp.st.discord-data'
  });
  dopplerSecrets = dopplerConfig();
  dopplerWriteOk = true;
  createConnection.mockClear();
  query.mockClear();
  end.mockClear();
  fetchSpy = spyOn(globalThis, 'fetch').mockImplementation(dopplerFetch as typeof fetch);
});

afterEach(() => {
  fetchSpy.mockRestore();
});

describe('admin database TLS', () => {
  test('fails closed before connecting when no CA is configured', async () => {
    delete privateEnv.DB_CA_CERT;

    await expect(
      revokeCredential('notifications', { dbUser: 'notifications_svc_old' })
    ).rejects.toThrow('DB admin CA certificate is not configured');

    expect(createConnection).not.toHaveBeenCalled();
  });

  test('uses the configured HeatWave CA', async () => {
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

describe('service account provisioning', () => {
  test('rotation creates the account on the VCN host with REQUIRE X509', async () => {
    const { dbUser } = await rotateCredential('users');

    expect(dbUser).toMatch(/^users_svc_r[0-9a-z]{8}$/);
    const account = `'${dbUser}'@'10.0.%'`;
    expect(sql()).toEqual([
      `CREATE USER IF NOT EXISTS ${account} IDENTIFIED BY ? REQUIRE X509`,
      `ALTER USER ${account} IDENTIFIED BY ? REQUIRE X509`,
      `REVOKE ALL PRIVILEGES, GRANT OPTION FROM ${account}`,
      `GRANT SELECT, INSERT, UPDATE, DELETE ON \`bagel_users\`.* TO ${account}`
    ]);
    expect(end).toHaveBeenCalledTimes(1);

    const [written] = dopplerWrites();
    expect(written).toMatchObject({ DB_USER: dbUser, DB_SCHEMA: 'bagel_users' });
    expect(written?.DB_PASS).toMatch(/^[\s\S]{32,128}$/);
  });

  test('set provisions the named user with the same host and TLS clause', async () => {
    const dbPass = 'Aa1!'.repeat(10);

    await setCredential('users', { dbUser: 'users_svc_main', dbPass });

    expect(sql()[0]).toBe(
      "CREATE USER IF NOT EXISTS 'users_svc_main'@'10.0.%' IDENTIFIED BY ? REQUIRE X509"
    );
    expect(sql()[1]).toBe("ALTER USER 'users_svc_main'@'10.0.%' IDENTIFIED BY ? REQUIRE X509");
    expect(query.mock.calls[0]?.[1]).toEqual([dbPass]);
    expect(dopplerWrites()).toEqual([{ DB_USER: 'users_svc_main', DB_PASS: dbPass, DB_SCHEMA: 'bagel_users' }]);
  });

  test('refuses to provision for a service with no client certificate in Doppler', async () => {
    dopplerSecrets = dopplerConfig({ DB_CLIENT_CERT: '', DB_CLIENT_KEY: '' });

    await expect(rotateCredential('users')).rejects.toThrow(
      'users has no DB_CLIENT_CERT/DB_CLIENT_KEY in Doppler'
    );
    await expect(
      setCredential('users', { dbUser: 'users_svc_main', dbPass: 'Aa1!'.repeat(10) })
    ).rejects.toThrow('REQUIRE X509 and lock the service out');

    expect(createConnection).not.toHaveBeenCalled();
    expect(dopplerWrites()).toEqual([]);
  });

  test('a half-configured client key pair counts as missing', async () => {
    dopplerSecrets = dopplerConfig({ DB_CLIENT_KEY: ' ' });

    await expect(rotateCredential('users')).rejects.toThrow('DB_CLIENT_CERT/DB_CLIENT_KEY');
    expect(createConnection).not.toHaveBeenCalled();
  });

  test('drops the new account when the Doppler write fails', async () => {
    dopplerWriteOk = false;

    await expect(rotateCredential('users')).rejects.toThrow('Doppler request failed (500)');

    const statements = sql();
    expect(statements.at(-1)).toMatch(/^DROP USER IF EXISTS 'users_svc_r[0-9a-z]{8}'@'10\.0\.%'$/);
    expect(end).toHaveBeenCalledTimes(2);
  });

  test('revoke targets the VCN host account', async () => {
    await revokeCredential('users', { dbUser: 'users_svc_old' });

    expect(sql()).toEqual([
      "REVOKE ALL PRIVILEGES, GRANT OPTION FROM 'users_svc_old'@'10.0.%'",
      "DROP USER IF EXISTS 'users_svc_old'@'10.0.%'"
    ]);
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});

describe('discord-data', () => {
  test('manages the discord_data_svc account through its underscore-named token', async () => {
    dopplerSecrets = dopplerConfig({ DB_USER: 'discord_data_svc_old' });

    const { dbUser } = await rotateCredential('discord-data');

    expect(dbUser).toMatch(/^discord_data_svc_r[0-9a-z]{8}$/);
    expect(sql()[3]).toBe(
      `GRANT SELECT, INSERT, UPDATE, DELETE ON \`bagel_discord\`.* TO '${dbUser}'@'10.0.%'`
    );
    for (const call of fetchSpy.mock.calls) {
      const headers = (call[1] as RequestInit).headers as Record<string, string>;
      expect(headers.authorization).toBe('Bearer dp.st.discord-data');
    }
    expect(dopplerWrites()).toEqual([
      expect.objectContaining({ DB_USER: dbUser, DB_SCHEMA: 'bagel_discord' })
    ]);
  });

  test('rejects the old discord_svc prefix', async () => {
    await expect(
      revokeCredential('discord-data', { dbUser: 'discord_svc_old' })
    ).rejects.toThrow('user must start with discord_data_svc');
    await expect(
      setCredential('discord-data', { dbUser: 'discord_svc_main', dbPass: 'Aa1!'.repeat(10) })
    ).rejects.toThrow('user must start with discord_data_svc');

    expect(createConnection).not.toHaveBeenCalled();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  test('stays unmanageable while its Doppler config carries no client certificate', async () => {
    dopplerSecrets = dopplerConfig({ DB_USER: 'discord_data_svc_old', DB_CLIENT_CERT: '', DB_CLIENT_KEY: '' });

    await expect(rotateCredential('discord-data')).rejects.toThrow(
      'discord-data has no DB_CLIENT_CERT/DB_CLIENT_KEY in Doppler (discord-data/prd)'
    );
    expect(createConnection).not.toHaveBeenCalled();
    expect(dopplerWrites()).toEqual([]);
  });
});
