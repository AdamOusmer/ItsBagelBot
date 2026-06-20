import { env } from '$env/dynamic/private';
import { nanoid } from 'nanoid';

import mysql from 'mysql2/promise';

export type DbServiceId = 'users' | 'commands' | 'modules' | 'transactions';

export type DbCredentialStatus = {
  id: DbServiceId;
  label: string;
  project: string;
  config: string;
  schema: string;
  expectedUserPrefix: string;
  dbUser: string;
  autoMigrate: string;
  canReadDoppler: boolean;
};

const services: Record<DbServiceId, Omit<DbCredentialStatus, 'dbUser' | 'autoMigrate' | 'canReadDoppler'>> = {
  users: {
    id: 'users',
    label: 'Users',
    project: 'users',
    config: 'prd',
    schema: 'bagel_users',
    expectedUserPrefix: 'users_svc'
  },
  commands: {
    id: 'commands',
    label: 'Commands',
    project: 'commands',
    config: 'prd',
    schema: 'bagel_commands',
    expectedUserPrefix: 'commands_svc'
  },
  modules: {
    id: 'modules',
    label: 'Modules',
    project: 'modules',
    config: 'prd',
    schema: 'bagel_modules',
    expectedUserPrefix: 'modules_svc'
  },
  transactions: {
    id: 'transactions',
    label: 'Transactions',
    project: 'transactions',
    config: 'prd',
    schema: 'bagel_transactions',
    expectedUserPrefix: 'transactions_svc'
  }
};

export function serviceIds(): DbServiceId[] {
  return Object.keys(services) as DbServiceId[];
}

export function serviceOf(raw: string): DbServiceId | null {
  return raw in services ? (raw as DbServiceId) : null;
}

export async function credentialStatuses(): Promise<DbCredentialStatus[]> {
  return Promise.all(
    serviceIds().map(async (id) => {
      const svc = services[id];
      try {
        const secrets = await dopplerSecrets(svc.project, svc.config);
        return {
          ...svc,
          dbUser: String(secrets.DB_USER ?? ''),
          autoMigrate: String(secrets.DB_AUTO_MIGRATE ?? ''),
          canReadDoppler: true
        };
      } catch {
        return {
          ...svc,
          dbUser: '',
          autoMigrate: '',
          canReadDoppler: false
        };
      }
    })
  );
}

export async function rotateCredential(id: DbServiceId): Promise<{ dbUser: string }> {
  const svc = services[id];
  const dbUser = `${svc.expectedUserPrefix}_r${Date.now().toString(36).slice(-8)}`;
  const dbPass = generatePassword();
  await provisionDbUser(dbUser, dbPass, svc.schema);
  try {
    await updateDoppler(svc.project, svc.config, {
      DB_USER: dbUser,
      DB_PASS: dbPass,
      DB_SCHEMA: svc.schema,
      DB_AUTO_MIGRATE: 'false',
      DB_MAX_OPEN_CONNS: '4',
      DB_QUERY_CONCURRENCY: '4'
    });
  } catch (e) {
    await dropDbUser(dbUser).catch(() => {});
    throw e;
  }
  return { dbUser };
}

export async function setCredential(
  id: DbServiceId,
  dbUser: string,
  dbPass: string
): Promise<{ dbUser: string }> {
  const svc = services[id];
  assertDbUser(dbUser);
  assertPassword(dbPass);
  if (dbUser === adminDbUser()) throw new Error('refusing to manage the admin database user');
  await provisionDbUser(dbUser, dbPass, svc.schema);
  await updateDoppler(svc.project, svc.config, {
    DB_USER: dbUser,
    DB_PASS: dbPass,
    DB_SCHEMA: svc.schema,
    DB_AUTO_MIGRATE: 'true',
    DB_MAX_OPEN_CONNS: '4',
    DB_QUERY_CONCURRENCY: '4'
  });
  return { dbUser };
}

export async function revokeCredential(id: DbServiceId, dbUser: string): Promise<{ dbUser: string }> {
  const svc = services[id];
  assertDbUser(dbUser);
  if (dbUser === adminDbUser()) throw new Error('refusing to revoke the admin database user');
  if (!dbUser.startsWith(svc.expectedUserPrefix)) {
    throw new Error(`user must start with ${svc.expectedUserPrefix}`);
  }
  const conn = await adminConnection();
  try {
    await conn.query(`REVOKE ALL PRIVILEGES, GRANT OPTION FROM ${accountSql(dbUser)}`);
    await conn.query(`DROP USER IF EXISTS ${accountSql(dbUser)}`);
  } finally {
    await conn.end();
  }
  return { dbUser };
}

async function dropDbUser(dbUser: string): Promise<void> {
  const conn = await adminConnection();
  try {
    await conn.query(`DROP USER IF EXISTS ${accountSql(dbUser)}`);
  } finally {
    await conn.end();
  }
}

async function provisionDbUser(dbUser: string, dbPass: string, schema: string): Promise<void> {
  const conn = await adminConnection();
  try {
    await conn.query(`CREATE USER IF NOT EXISTS ${accountSql(dbUser)} IDENTIFIED BY ?`, [dbPass]);
    await conn.query(`ALTER USER ${accountSql(dbUser)} IDENTIFIED BY ?`, [dbPass]);
    await conn.query(`REVOKE ALL PRIVILEGES, GRANT OPTION FROM ${accountSql(dbUser)}`).catch(() => {});
    await conn.query(`GRANT SELECT, INSERT, UPDATE, DELETE ON ${schemaSql(schema)}.* TO ${accountSql(dbUser)}`);
  } finally {
    await conn.end();
  }
}

async function adminConnection(): Promise<mysql.Connection> {
  const hostPort = env.DB_ADMIN_ADDR ?? env.DB_ADDR;
  const host = env.DB_ADMIN_HOST ?? hostPort?.split(':')[0];
  const port = Number(env.DB_ADMIN_PORT ?? hostPort?.split(':')[1] ?? 3306);
  const user = adminDbUser();
  const password = env.DB_ADMIN_PASS ?? env.DB_ADMIN_PASSWORD;
  if (!host || !user || !password) {
    throw new Error('DB admin credential is not configured');
  }
  const ca = env.DB_ADMIN_CA_CERT ?? env.DB_CA_CERT;
  return mysql.createConnection({
    host,
    port,
    user,
    password,
    ssl: ca ? { ca, minVersion: 'TLSv1.2' } : { rejectUnauthorized: false, minVersion: 'TLSv1.2' },
    connectTimeout: 5000,
    multipleStatements: false
  });
}

function adminDbUser(): string {
  return env.DB_ADMIN_USER ?? '';
}

async function dopplerSecrets(project: string, config: string): Promise<Record<string, string>> {
  const res = await dopplerFetch(
    `/v3/configs/config/secrets?project=${encodeURIComponent(project)}&config=${encodeURIComponent(config)}`
  );
  const body = (await res.json()) as { secrets?: Record<string, { computed?: string; raw?: string } | string> };
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(body.secrets ?? {})) {
    out[key] = typeof value === 'string' ? value : String(value.computed ?? value.raw ?? '');
  }
  return out;
}

async function updateDoppler(project: string, config: string, secrets: Record<string, string>): Promise<void> {
  const res = await dopplerFetch('/v3/configs/config/secrets', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ project, config, secrets })
  });
  if (!res.ok) throw new Error(`Doppler update failed (${res.status})`);
}

async function dopplerFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const token = env.DOPPLER_MANAGEMENT_TOKEN ?? env.DOPPLER_TOKEN;
  if (!token) throw new Error('Doppler management token is not configured');
  const res = await fetch(`https://api.doppler.com${path}`, {
    ...init,
    headers: {
      accept: 'application/json',
      authorization: `Bearer ${token}`,
      ...(init.headers ?? {})
    }
  });
  if (!res.ok) throw new Error(`Doppler request failed (${res.status})`);
  return res;
}

function generatePassword(): string {
  let pass = '';
  do {
    pass = nanoid(40);
  } while (
    !/[A-Z]/.test(pass) ||
    !/[a-z]/.test(pass) ||
    !/[0-9]/.test(pass) ||
    !/[^A-Za-z0-9]/.test(pass)
  );
  return pass;
}

function assertDbUser(dbUser: string): void {
  if (!/^[A-Za-z0-9_]{3,32}$/.test(dbUser)) {
    throw new Error('database user must be 3-32 characters of letters, numbers, or underscore');
  }
}

function assertPassword(dbPass: string): void {
  if (dbPass.length < 32 || dbPass.length > 128) {
    throw new Error('password must be 32-128 characters');
  }
}

function accountSql(dbUser: string): string {
  assertDbUser(dbUser);
  return `'${dbUser}'@'%'`;
}

function schemaSql(schema: string): string {
  if (!/^bagel_(users|commands|modules|transactions)$/.test(schema)) {
    throw new Error('invalid database schema');
  }
  return `\`${schema}\``;
}
