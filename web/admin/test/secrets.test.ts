// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { afterEach, beforeEach, describe, expect, mock, spyOn, test } from 'bun:test';

const privateEnv: Record<string, string | undefined> = {};
let partialRevokes: number | undefined = 0;
let rejectedSql = '';
const query = mock(async (statement: string, _params?: unknown[]) => {
  if (statement === rejectedSql) throw new Error('SQL statement rejected');
  const rows = statement.startsWith('SELECT @@GLOBAL.partial_revokes') ? [{ partialRevokes }] : [];
  return [rows, []];
});
const end = mock(async () => {});
const createConnection = mock(async (_options: unknown) => ({ query, end }));

mock.module('@bagel/kit/server/env', () => ({ env: privateEnv }));
mock.module('mysql2/promise', () => ({ default: { createConnection } }));

const { credentialStatuses, revokeCredential, rotateCredential, setCredential } = await import(
  '../src/lib/server/secrets'
);

type DopplerSecrets = Record<string, string>;

function dopplerConfig(overrides: DopplerSecrets = {}): DopplerSecrets {
  return { DB_SCHEMA: 'users_data', DB_USER: 'users_svc_old', DB_CLIENT_CERT: 'cert-pem', DB_CLIENT_KEY: 'key-pem', ...overrides };
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
  });
  dopplerSecrets = dopplerConfig();
  dopplerWriteOk = true;
  partialRevokes = 0;
  rejectedSql = '';
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
      'SELECT @@GLOBAL.partial_revokes AS partialRevokes',
      `CREATE USER IF NOT EXISTS ${account} IDENTIFIED BY ? REQUIRE X509`,
      `ALTER USER ${account} IDENTIFIED BY ? REQUIRE X509`,
      `REVOKE ALL PRIVILEGES, GRANT OPTION FROM ${account}`,
      `GRANT SELECT, INSERT, UPDATE, DELETE ON \`users\\_data\`.* TO ${account}`
    ]);
    expect(end).toHaveBeenCalledTimes(1);

    const [written] = dopplerWrites();
    expect(written).toEqual({ DB_USER: dbUser, DB_PASS: expect.any(String) });
    expect(written?.DB_PASS).toMatch(/^[\s\S]{32,128}$/);
  });

  test('set provisions the named user with the same host and TLS clause', async () => {
    const dbPass = 'Aa1!'.repeat(10);

    await setCredential('users', { dbUser: 'users_svc_main', dbPass });

    expect(sql()[1]).toBe(
      "CREATE USER IF NOT EXISTS 'users_svc_main'@'10.0.%' IDENTIFIED BY ? REQUIRE X509"
    );
    expect(sql()[2]).toBe("ALTER USER 'users_svc_main'@'10.0.%' IDENTIFIED BY ? REQUIRE X509");
    expect(query.mock.calls[1]?.[1]).toEqual([dbPass]);
    expect(dopplerWrites()).toEqual([{ DB_USER: 'users_svc_main', DB_PASS: dbPass }]);
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

  test('propagates a failed Doppler write without undoing SQL changes', async () => {
    dopplerWriteOk = false;

    await expect(rotateCredential('users')).rejects.toThrow('Doppler request failed (500)');

    const statements = sql();
    expect(statements.at(-1)).toMatch(/^GRANT SELECT, INSERT, UPDATE, DELETE ON/);
    expect(statements.some((statement) => statement.startsWith('DROP USER'))).toBe(false);
    expect(end).toHaveBeenCalledTimes(1);
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

describe('configured database schema', () => {
  test.each([
    { schema: 'users', partialRevokes: 0, quoted: '`users`' },
    { schema: 'users_data', partialRevokes: 0, quoted: '`users\\_data`' },
    { schema: 'users_data', partialRevokes: 1, quoted: '`users_data`' }
  ])('grants only $schema with partial_revokes=$partialRevokes', async (scenario) => {
    dopplerSecrets = dopplerConfig({ DB_SCHEMA: scenario.schema });
    partialRevokes = scenario.partialRevokes;

    await setCredential('users', { dbUser: 'users_svc_main', dbPass: 'Aa1!'.repeat(10) });

    expect(sql().at(-1)).toBe(`GRANT SELECT, INSERT, UPDATE, DELETE ON ${scenario.quoted}.* TO 'users_svc_main'@'10.0.%'`);
    expect(dopplerWrites()[0]).not.toHaveProperty('DB_SCHEMA');
  });

  test.each([undefined, '', ' ', 'users%', 'users.*', 'users`', 'users\\data', 'users-data', 'u'.repeat(65)])(
    'refuses missing or unsafe schema %j before connecting', async (schema) => {
      dopplerSecrets = dopplerConfig();
      delete dopplerSecrets.DB_SCHEMA;
      Object.assign(dopplerSecrets, schema === undefined ? {} : { DB_SCHEMA: schema });

      await expect(rotateCredential('users')).rejects.toThrow('DB_SCHEMA');
      await expect(setCredential('users', { dbUser: 'users_svc_main', dbPass: 'Aa1!'.repeat(10) })).rejects.toThrow('DB_SCHEMA');
      expect({ connections: createConnection.mock.calls.length, writes: dopplerWrites() }).toEqual({ connections: 0, writes: [] });
    }
  );

  test('refuses unknown server grant semantics before SQL mutations', async () => {
    partialRevokes = undefined;

    await expect(rotateCredential('users')).rejects.toThrow('could not determine MySQL schema grant semantics');

    expect(sql()).toEqual(['SELECT @@GLOBAL.partial_revokes AS partialRevokes']);
    expect(dopplerWrites()).toEqual([]);
  });

  test('propagates revoke failures before granting or writing Doppler', async () => {
    rejectedSql = "REVOKE ALL PRIVILEGES, GRANT OPTION FROM 'users_svc_main'@'10.0.%'";

    await expect(setCredential('users', { dbUser: 'users_svc_main', dbPass: 'Aa1!'.repeat(10) })).rejects.toThrow('SQL statement rejected');

    expect(sql().at(-1)).toBe(rejectedSql);
    expect(dopplerWrites()).toEqual([]);
  });

  test('reports the configured schema in credential status', async () => {
    dopplerSecrets = dopplerConfig({ DB_SCHEMA: 'users_custom' });

    const statuses = await credentialStatuses();

    expect(statuses.find((status) => status.id === 'users')).toMatchObject({ schema: 'users_custom', canReadDoppler: true });
  });
});
