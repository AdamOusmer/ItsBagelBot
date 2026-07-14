// Secrets console backend: per-service database credentials plus Doppler
// service-token minting, on least-privileged Doppler access.
//
// Token model (least privilege):
//   DOPPLER_TOKEN_<SERVICE>   — per-service Doppler token scoped to that one
//                               project (users/commands/…). Preferred.
//   DOPPLER_MANAGEMENT_TOKEN  — legacy broad token, used only as a fallback
//                               and flagged as over-privileged in the UI.
// Minted service tokens are always read-only and scoped to a single config —
// the narrowest credential Doppler can issue.
import { env } from '$env/dynamic/private';
import { nanoid } from 'nanoid';
import mysql from 'mysql2/promise';

export type SecretServiceId = 'users' | 'commands' | 'modules' | 'transactions' | 'notifications';

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
  notifications: { id: 'notifications', label: 'Notifications', project: 'notifications', config: 'prd', schema: 'bagel_notifications', expectedUserPrefix: 'notifications_svc' }
};

export function serviceIds(): SecretServiceId[] {
  return Object.keys(services) as SecretServiceId[];
}

export function serviceOf(raw: string): SecretServiceId | null {
  return raw in services ? (raw as SecretServiceId) : null;
}

// ── Doppler token resolution ─────────────────────────────────────────────────

export type TokenSource = 'scoped' | 'legacy' | 'missing';

function legacyToken(): string {
  return (env.DOPPLER_MANAGEMENT_TOKEN ?? env.DOPPLER_TOKEN ?? '').trim();
}

// tokenFor picks the narrowest credential available for a service and reports
// which tier it came from, so the UI can tell the truth about privilege.
export function tokenFor(svc: ServiceDef): { token: string; source: TokenSource } {
  const scoped = (env[`DOPPLER_TOKEN_${svc.id.toUpperCase()}`] ?? '').trim();
  if (scoped) return { token: scoped, source: 'scoped' };
  const legacy = legacyToken();
  if (legacy) return { token: legacy, source: 'legacy' };
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

// Doppler POST/DELETE bodies always carry the project/config pair; centralize
// the JSON envelope so call sites stay declarative.
function dopplerBody(svc: ServiceDef, extra: Record<string, unknown>): RequestInit {
  return {
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ project: svc.project, config: svc.config, ...extra })
  };
}

// ── Scope report ─────────────────────────────────────────────────────────────

export interface ScopeReport {
  // Which tier each service resolves to.
  sources: Record<SecretServiceId, TokenSource>;
  // True when at least one service still falls back to the broad legacy token.
  legacyInUse: boolean;
  // Projects visible to the legacy token beyond the five service projects —
  // concrete evidence of over-privilege ([] when unknown or clean).
  legacyExcessProjects: string[];
}

async function visibleProjects(token: string): Promise<string[] | null> {
  try {
    const res = await dopplerFetch({ token, path: '/v3/projects?per_page=100' });
    const body = (await res.json()) as { projects?: { slug?: string; name?: string }[] };
    return (body.projects ?? []).map((p) => p.slug ?? p.name ?? '').filter(Boolean);
  } catch {
    // A properly scoped token often cannot list projects at all; that is not
    // an error worth surfacing, just an unknown.
    return null;
  }
}

export async function scopeReport(): Promise<ScopeReport> {
  const sources = Object.fromEntries(
    serviceIds().map((id) => [id, tokenFor(services[id]).source])
  ) as Record<SecretServiceId, TokenSource>;

  const legacyInUse = Object.values(sources).includes('legacy');
  let legacyExcessProjects: string[] = [];
  if (legacyInUse) {
    const allowed = new Set(serviceIds().map((id) => services[id].project));
    const visible = await visibleProjects(legacyToken());
    legacyExcessProjects = (visible ?? []).filter((p) => !allowed.has(p));
  }
  return { sources, legacyInUse, legacyExcessProjects };
}

// ── Config secrets (DB credential status) ────────────────────────────────────

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

// ── Doppler service tokens (mint / list / revoke) ────────────────────────────

export interface ServiceTokenView {
  slug: string;
  name: string;
  createdAt: string;
  lastSeenAt: string | null;
  expiresAt: string | null;
}

interface ServiceTokenWire {
  slug?: string;
  name?: string;
  created_at?: string;
  last_seen_at?: string | null;
  expires_at?: string | null;
}

function tokenViewOf(t: ServiceTokenWire): ServiceTokenView {
  return {
    slug: t.slug ?? '',
    name: t.name ?? '',
    createdAt: t.created_at ?? '',
    lastSeenAt: t.last_seen_at ?? null,
    expiresAt: t.expires_at ?? null
  };
}

export async function listServiceTokens(id: SecretServiceId): Promise<ServiceTokenView[]> {
  const svc = services[id];
  const res = await dopplerFetch({
    token: tokenFor(svc).token,
    path: `/v3/configs/config/tokens?${configQuery(svc)}`
  });
  const body = (await res.json()) as { tokens?: ServiceTokenWire[] };
  return (body.tokens ?? []).map(tokenViewOf);
}

// One validator over a rule table instead of a bespoke assert per field.
interface FormatRule {
  re: RegExp;
  message: string;
}

export const FORMATS = {
  tokenName: {
    re: /^[a-z0-9][a-z0-9-]{2,47}$/,
    message: 'token name must be 3-48 chars: lowercase letters, digits, dashes'
  },
  tokenSlug: { re: /^[A-Za-z0-9_-]{4,64}$/, message: 'invalid token slug' },
  dbUser: {
    re: /^[A-Za-z0-9_]{3,32}$/,
    message: 'database user must be 3-32 characters of letters, numbers, or underscore'
  },
  dbPassword: { re: /^[\s\S]{32,128}$/, message: 'password must be 32-128 characters' }
} satisfies Record<string, FormatRule>;

export function assertFormat(value: string, rule: FormatRule): void {
  if (!rule.re.test(value)) throw new Error(rule.message);
}

export interface MintTokenInput {
  name: string;
  expireDays: number;
}

// mintServiceToken issues a READ-ONLY token scoped to one service's config —
// the least-privileged credential Doppler can hand out. The key is returned
// exactly once; it is never stored server-side.
export async function mintServiceToken(
  id: SecretServiceId,
  input: MintTokenInput
): Promise<{ key: string; token: ServiceTokenView }> {
  const svc = services[id];
  assertFormat(input.name, FORMATS.tokenName);
  const days = Math.min(Math.max(Math.trunc(input.expireDays), 0), 365);
  const res = await dopplerFetch({
    token: tokenFor(svc).token,
    path: '/v3/configs/config/tokens',
    init: {
      method: 'POST',
      ...dopplerBody(svc, {
        name: input.name,
        access: 'read',
        ...(days > 0 ? { expire_at: new Date(Date.now() + days * 864e5).toISOString() } : {})
      })
    }
  });
  const body = (await res.json()) as { token?: ServiceTokenWire & { key?: string } };
  if (!body.token?.key) throw new Error('Doppler did not return a token key');
  return { key: body.token.key, token: tokenViewOf(body.token) };
}

export async function revokeServiceToken(id: SecretServiceId, input: { slug: string }): Promise<void> {
  const svc = services[id];
  assertFormat(input.slug, FORMATS.tokenSlug);
  await dopplerFetch({
    token: tokenFor(svc).token,
    path: '/v3/configs/config/tokens/token',
    init: { method: 'DELETE', ...dopplerBody(svc, { slug: input.slug }) }
  });
}

// ── MySQL runtime users ──────────────────────────────────────────────────────

export interface DbCredentialInput {
  dbUser: string;
  dbPass: string;
}

// dbEnvOf builds the Doppler payload a service reads its database credential
// from; autoMigrate differs between rotate (false) and manual set (true).
function dbEnvOf(cred: DbCredentialInput, svc: ServiceDef, autoMigrate: boolean): Record<string, string> {
  return {
    DB_USER: cred.dbUser,
    DB_PASS: cred.dbPass,
    DB_SCHEMA: svc.schema,
    DB_AUTO_MIGRATE: String(autoMigrate),
    DB_MAX_OPEN_CONNS: '4',
    DB_QUERY_CONCURRENCY: '4'
  };
}

// assertManageable rejects credentials this console must never touch: the
// privileged admin user itself, and (for revokes) users outside the service's
// namespace.
function assertManageable(cred: Pick<DbCredentialInput, 'dbUser'>, svc?: ServiceDef): void {
  assertFormat(cred.dbUser, FORMATS.dbUser);
  if (cred.dbUser === adminDbUser()) throw new Error('refusing to manage the admin database user');
  if (svc && !cred.dbUser.startsWith(svc.expectedUserPrefix)) {
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
    await updateDoppler(svc, dbEnvOf(cred, svc, false));
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
  assertManageable(cred);
  assertFormat(cred.dbPass, FORMATS.dbPassword);
  await provisionDbUser(cred, svc);
  await updateDoppler(svc, dbEnvOf(cred, svc, true));
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
}

// envFirst returns the first non-empty value among the named env keys.
function envFirst(...keys: string[]): string {
  for (const key of keys) {
    const value = env[key];
    if (value) return value;
  }
  return '';
}

const REQUIRED_TARGET_FIELDS = ['host', 'user', 'password'] as const;

// adminDbTarget resolves the privileged MySQL endpoint from env, in one place,
// and fails with one clear message when any required part is missing.
function adminDbTarget(): DbAdminTarget {
  const [addrHost = '', addrPort = ''] = envFirst('DB_ADMIN_ADDR', 'DB_ADDR').split(':');
  const target: DbAdminTarget = {
    host: envFirst('DB_ADMIN_HOST') || addrHost,
    port: Number(envFirst('DB_ADMIN_PORT') || addrPort || 3306),
    user: adminDbUser(),
    password: envFirst('DB_ADMIN_PASS', 'DB_ADMIN_PASSWORD'),
    ca: envFirst('DB_ADMIN_CA_CERT', 'DB_CA_CERT')
  };
  const missing = REQUIRED_TARGET_FIELDS.some((field) => !target[field]);
  if (missing) throw new Error('DB admin credential is not configured');
  return target;
}

async function adminConnection(): Promise<mysql.Connection> {
  const target = adminDbTarget();
  return mysql.createConnection({
    host: target.host,
    port: target.port,
    user: target.user,
    password: target.password,
    ssl: target.ca
      ? { ca: target.ca, minVersion: 'TLSv1.2' }
      : { rejectUnauthorized: false, minVersion: 'TLSv1.2' },
    connectTimeout: 5000,
    multipleStatements: false
  });
}

function adminDbUser(): string {
  return env.DB_ADMIN_USER ?? '';
}

// Every character class a strong password must hit at least once.
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
  if (!/^bagel_(users|commands|modules|transactions|notifications)$/.test(svc.schema)) {
    throw new Error('invalid database schema');
  }
  return `\`${svc.schema}\``;
}
