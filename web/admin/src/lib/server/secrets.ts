// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// No broad-token fallback: a missing scoped DOPPLER_TOKEN_<SERVICE> must stay missing.
import { env } from '$env/dynamic/private';
import { nanoid } from 'nanoid';
import mysql from 'mysql2/promise';

export type SecretServiceId =
  | 'users'
  | 'commands'
  | 'modules'
  | 'transactions'
  | 'notifications'
  | 'discord-data';

interface ServiceDef {
  id: SecretServiceId;
  label: string;
  project: string;
  config: string;
  schema: string;
  expectedUserPrefix: string;
}

const services: Record<SecretServiceId, ServiceDef> = {
  users: { id: 'users', label: 'Users', project: 'users', config: 'prd', schema: 'bagel_users', expectedUserPrefix: 'users_svc' },
  commands: { id: 'commands', label: 'Commands', project: 'commands', config: 'prd', schema: 'bagel_commands', expectedUserPrefix: 'commands_svc' },
  modules: { id: 'modules', label: 'Modules', project: 'modules', config: 'prd', schema: 'bagel_modules', expectedUserPrefix: 'modules_svc' },
  transactions: { id: 'transactions', label: 'Transactions', project: 'transactions', config: 'prd', schema: 'bagel_transactions', expectedUserPrefix: 'transactions_svc' },
  notifications: { id: 'notifications', label: 'Notifications', project: 'notifications', config: 'prd', schema: 'bagel_notifications', expectedUserPrefix: 'notifications_svc' },
  'discord-data': { id: 'discord-data', label: 'Discord data', project: 'discord-data', config: 'prd', schema: 'bagel_discord', expectedUserPrefix: 'discord_svc' }
};

export function serviceIds(): SecretServiceId[] {
  return Object.keys(services) as SecretServiceId[];
}

export function serviceOf(raw: string): SecretServiceId | null {
  return raw in services ? (raw as SecretServiceId) : null;
}

export type TokenSource = 'scoped' | 'missing';

export function tokenFor(svc: ServiceDef): { token: string; source: TokenSource } {
  const scoped = (env[`DOPPLER_TOKEN_${svc.id.toUpperCase()}`] ?? '').trim();
  if (scoped) return { token: scoped, source: 'scoped' };
  return { token: '', source: 'missing' };
}

interface DopplerCall {
  token: string;
  path: string;
  init?: RequestInit;
}

async function dopplerFetch({ token, path, init = {} }: DopplerCall): Promise<Response> {
  if (!token) throw new Error('no Doppler token configured for this service');
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

function dopplerBody(svc: ServiceDef, extra: Record<string, unknown>): RequestInit {
  return {
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ project: svc.project, config: svc.config, ...extra })
  };
}

export interface ScopeReport {
  sources: Record<SecretServiceId, TokenSource>;
}

export async function scopeReport(): Promise<ScopeReport> {
  const sources = Object.fromEntries(
    serviceIds().map((id) => [id, tokenFor(services[id]).source])
  ) as Record<SecretServiceId, TokenSource>;
  return { sources };
}

export interface DbCredentialStatus {
  id: SecretServiceId;
  label: string;
  project: string;
  config: string;
  schema: string;
  expectedUserPrefix: string;
  dbUser: string;
  autoMigrate: string;
  canReadDoppler: boolean;
  tokenSource: TokenSource;
}

function configQuery(svc: ServiceDef): string {
  return `project=${encodeURIComponent(svc.project)}&config=${encodeURIComponent(svc.config)}`;
}

async function dopplerSecrets(svc: ServiceDef): Promise<Record<string, string>> {
  const res = await dopplerFetch({
    token: tokenFor(svc).token,
    path: `/v3/configs/config/secrets?${configQuery(svc)}`
  });
  const body = (await res.json()) as {
    secrets?: Record<string, { computed?: string; raw?: string } | string>;
  };
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(body.secrets ?? {})) {
    out[key] = typeof value === 'string' ? value : String(value.computed ?? value.raw ?? '');
  }
  return out;
}

async function updateDoppler(svc: ServiceDef, secrets: Record<string, string>): Promise<void> {
  await dopplerFetch({
    token: tokenFor(svc).token,
    path: '/v3/configs/config/secrets',
    init: { method: 'POST', ...dopplerBody(svc, { secrets }) }
  });
}

export async function credentialStatuses(): Promise<DbCredentialStatus[]> {
  return Promise.all(
    serviceIds().map(async (id) => {
      const svc = services[id];
      const source = tokenFor(svc).source;
      try {
        const secrets = await dopplerSecrets(svc);
        return {
          ...svc,
          dbUser: String(secrets.DB_USER ?? ''),
          autoMigrate: String(secrets.DB_AUTO_MIGRATE ?? ''),
          canReadDoppler: true,
          tokenSource: source
        };
      } catch {
        return { ...svc, dbUser: '', autoMigrate: '', canReadDoppler: false, tokenSource: source };
      }
    })
  );
}

interface FormatRule {
  re: RegExp;
  message: string;
}

const FORMATS = {
  dbUser: {
    re: /^[A-Za-z0-9_]{3,32}$/,
    message: 'database user must be 3-32 characters of letters, numbers, or underscore'
  },
  dbPassword: { re: /^[\s\S]{32,128}$/, message: 'password must be 32-128 characters' }
} satisfies Record<string, FormatRule>;

function assertFormat(value: string, rule: FormatRule): void {
  if (!rule.re.test(value)) throw new Error(rule.message);
}

export interface DbCredentialInput {
  dbUser: string;
  dbPass: string;
}

function dbEnvOf(cred: DbCredentialInput, svc: ServiceDef): Record<string, string> {
  return {
    DB_USER: cred.dbUser,
    DB_PASS: cred.dbPass,
    DB_SCHEMA: svc.schema
  };
}

function assertManageable(cred: Pick<DbCredentialInput, 'dbUser'>, svc: ServiceDef): void {
  assertFormat(cred.dbUser, FORMATS.dbUser);
  if (cred.dbUser === adminDbUser()) throw new Error('refusing to manage the admin database user');
  if (!cred.dbUser.startsWith(svc.expectedUserPrefix)) {
    throw new Error(`user must start with ${svc.expectedUserPrefix}`);
  }
}

export async function rotateCredential(id: SecretServiceId): Promise<{ dbUser: string }> {
  const svc = services[id];
  const cred: DbCredentialInput = {
    dbUser: `${svc.expectedUserPrefix}_r${Date.now().toString(36).slice(-8)}`,
    dbPass: generatePassword()
  };
  await provisionDbUser(cred, svc);
  try {
    await updateDoppler(svc, dbEnvOf(cred, svc));
  } catch (e) {
    await dropDbUser(cred).catch(() => {});
    throw e;
  }
  return { dbUser: cred.dbUser };
}

export async function setCredential(
  id: SecretServiceId,
  cred: DbCredentialInput
): Promise<{ dbUser: string }> {
  const svc = services[id];
  assertManageable(cred, svc);
  assertFormat(cred.dbPass, FORMATS.dbPassword);
  await provisionDbUser(cred, svc);
  await updateDoppler(svc, dbEnvOf(cred, svc));
  return { dbUser: cred.dbUser };
}

export async function revokeCredential(
  id: SecretServiceId,
  input: Pick<DbCredentialInput, 'dbUser'>
): Promise<{ dbUser: string }> {
  const svc = services[id];
  assertManageable(input, svc);
  const account = accountSql(input);
  const conn = await adminConnection();
  try {
    await conn.query(`REVOKE ALL PRIVILEGES, GRANT OPTION FROM ${account}`);
    await conn.query(`DROP USER IF EXISTS ${account}`);
  } finally {
    await conn.end();
  }
  return { dbUser: input.dbUser };
}

async function dropDbUser(cred: Pick<DbCredentialInput, 'dbUser'>): Promise<void> {
  const conn = await adminConnection();
  try {
    await conn.query(`DROP USER IF EXISTS ${accountSql(cred)}`);
  } finally {
    await conn.end();
  }
}

async function provisionDbUser(cred: DbCredentialInput, svc: ServiceDef): Promise<void> {
  const account = accountSql(cred);
  const conn = await adminConnection();
  try {
    await conn.query(`CREATE USER IF NOT EXISTS ${account} IDENTIFIED BY ?`, [cred.dbPass]);
    await conn.query(`ALTER USER ${account} IDENTIFIED BY ?`, [cred.dbPass]);
    await conn.query(`REVOKE ALL PRIVILEGES, GRANT OPTION FROM ${account}`).catch(() => {});
    await conn.query(`GRANT SELECT, INSERT, UPDATE, DELETE ON ${schemaSql(svc)}.* TO ${account}`);
  } finally {
    await conn.end();
  }
}

interface DbAdminTarget {
  host: string;
  port: number;
  user: string;
  password: string;
  ca: string;
  clientCert: string;
  clientKey: string;
}

function envFirst(...keys: string[]): string {
  for (const key of keys) {
    const value = env[key];
    if (value) return value;
  }
  return '';
}

const REQUIRED_TARGET_FIELDS = ['host', 'user', 'password'] as const;

function adminDbEndpoint(): { host: string; port: number } {
  const [addrHost = '', addrPort = ''] = envFirst('DB_ADMIN_ADDR', 'DB_ADDR').split(':');
  return {
    host: envFirst('DB_ADMIN_HOST') || addrHost,
    port: Number(envFirst('DB_ADMIN_PORT') || addrPort || 3306)
  };
}

function adminClientIdentity(): { clientCert: string; clientKey: string } {
  const clientCert = envFirst('DB_ADMIN_CLIENT_CERT');
  const clientKey = envFirst('DB_ADMIN_CLIENT_KEY');
  if (!clientCert !== !clientKey)
    throw new Error('DB_ADMIN_CLIENT_CERT and DB_ADMIN_CLIENT_KEY must be set together');
  return { clientCert, clientKey };
}

function adminDbTarget(): DbAdminTarget {
  const target: DbAdminTarget = {
    ...adminDbEndpoint(),
    user: adminDbUser(),
    password: envFirst('DB_ADMIN_PASS', 'DB_ADMIN_PASSWORD'),
    // Must be the dedicated HeatWave CA: anything a broader CA signed could impersonate the DB.
    ca: envFirst('DB_ADMIN_CA_CERT', 'DB_CA_CERT'),
    ...adminClientIdentity()
  };
  const missing = REQUIRED_TARGET_FIELDS.some((field) => !target[field]);
  if (missing) throw new Error('DB admin credential is not configured');
  if (!target.ca) throw new Error('DB admin CA certificate is not configured');
  return target;
}

async function adminConnection(): Promise<mysql.Connection> {
  const target = adminDbTarget();
  return mysql.createConnection({
    host: target.host,
    port: target.port,
    user: target.user,
    password: target.password,
    ssl: {
      ca: target.ca,
      rejectUnauthorized: true,
      minVersion: 'TLSv1.2',
      ...(target.clientCert ? { cert: target.clientCert, key: target.clientKey } : {})
    },
    connectTimeout: 5000,
    multipleStatements: false
  });
}

function adminDbUser(): string {
  return env.DB_ADMIN_USER ?? '';
}

const PASSWORD_CLASSES = [/[A-Z]/, /[a-z]/, /[0-9]/, /[^A-Za-z0-9]/];

function generatePassword(): string {
  let pass = nanoid(40);
  while (!PASSWORD_CLASSES.every((cls) => cls.test(pass))) pass = nanoid(40);
  return pass;
}

function accountSql(cred: Pick<DbCredentialInput, 'dbUser'>): string {
  assertFormat(cred.dbUser, FORMATS.dbUser);
  return `'${cred.dbUser}'@'%'`;
}

function schemaSql(svc: ServiceDef): string {
  // Allowlist, not an escape: the name is interpolated into DDL. Each service above needs an arm.
  if (!/^bagel_(users|commands|modules|transactions|notifications|discord)$/.test(svc.schema)) {
    throw new Error('invalid database schema');
  }
  return `\`${svc.schema}\``;
}
