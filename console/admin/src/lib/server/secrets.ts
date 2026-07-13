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

function scopedToken(id: SecretServiceId): string {
  return (env[`DOPPLER_TOKEN_${id.toUpperCase()}`] ?? '').trim();
}

function legacyToken(): string {
  return (env.DOPPLER_MANAGEMENT_TOKEN ?? env.DOPPLER_TOKEN ?? '').trim();
}

// tokenFor picks the narrowest credential available for a service and reports
// which tier it came from, so the UI can tell the truth about privilege.
export function tokenFor(id: SecretServiceId): { token: string; source: TokenSource } {
  const scoped = scopedToken(id);
  if (scoped) return { token: scoped, source: 'scoped' };
  const legacy = legacyToken();
  if (legacy) return { token: legacy, source: 'legacy' };
  return { token: '', source: 'missing' };
}

async function dopplerFetch(token: string, path: string, init: RequestInit = {}): Promise<Response> {
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
    const res = await dopplerFetch(token, '/v3/projects?per_page=100');
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
    serviceIds().map((id) => [id, tokenFor(id).source])
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

async function dopplerSecrets(id: SecretServiceId): Promise<Record<string, string>> {
  const svc = services[id];
  const res = await dopplerFetch(
    tokenFor(id).token,
    `/v3/configs/config/secrets?project=${encodeURIComponent(svc.project)}&config=${encodeURIComponent(svc.config)}`
  );
  const body = (await res.json()) as {
    secrets?: Record<string, { computed?: string; raw?: string } | string>;
  };
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(body.secrets ?? {})) {
    out[key] = typeof value === 'string' ? value : String(value.computed ?? value.raw ?? '');
  }
  return out;
}

async function updateDoppler(id: SecretServiceId, secrets: Record<string, string>): Promise<void> {
  const svc = services[id];
  await dopplerFetch(tokenFor(id).token, '/v3/configs/config/secrets', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ project: svc.project, config: svc.config, secrets })
  });
}

export async function credentialStatuses(): Promise<DbCredentialStatus[]> {
  return Promise.all(
    serviceIds().map(async (id) => {
      const svc = services[id];
      const source = tokenFor(id).source;
      try {
        const secrets = await dopplerSecrets(id);
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
  const res = await dopplerFetch(
    tokenFor(id).token,
    `/v3/configs/config/tokens?project=${encodeURIComponent(svc.project)}&config=${encodeURIComponent(svc.config)}`
  );
  const body = (await res.json()) as { tokens?: ServiceTokenWire[] };
  return (body.tokens ?? []).map(tokenViewOf);
}

const TOKEN_NAME_RE = /^[a-z0-9][a-z0-9-]{2,47}$/;

export function assertTokenName(name: string): void {
  if (!TOKEN_NAME_RE.test(name)) {
    throw new Error('token name must be 3-48 chars: lowercase letters, digits, dashes');
  }
}

// mintServiceToken issues a READ-ONLY token scoped to one service's config —
// the least-privileged credential Doppler can hand out. The key is returned
// exactly once; it is never stored server-side.
export async function mintServiceToken(
  id: SecretServiceId,
  name: string,
  expireDays: number
): Promise<{ key: string; token: ServiceTokenView }> {
  const svc = services[id];
  assertTokenName(name);
  const days = Math.min(Math.max(Math.trunc(expireDays), 0), 365);
  const res = await dopplerFetch(tokenFor(id).token, '/v3/configs/config/tokens', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      project: svc.project,
      config: svc.config,
      name,
      access: 'read',
      ...(days > 0 ? { expire_at: new Date(Date.now() + days * 864e5).toISOString() } : {})
    })
  });
  const body = (await res.json()) as { token?: ServiceTokenWire & { key?: string } };
  if (!body.token?.key) throw new Error('Doppler did not return a token key');
  return { key: body.token.key, token: tokenViewOf(body.token) };
}

export async function revokeServiceToken(id: SecretServiceId, slug: string): Promise<void> {
  const svc = services[id];
  if (!/^[A-Za-z0-9_-]{4,64}$/.test(slug)) throw new Error('invalid token slug');
  await dopplerFetch(tokenFor(id).token, '/v3/configs/config/tokens/token', {
    method: 'DELETE',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ project: svc.project, config: svc.config, slug })
  });
}

// ── MySQL runtime users ──────────────────────────────────────────────────────

export async function rotateCredential(id: SecretServiceId): Promise<{ dbUser: string }> {
  const svc = services[id];
  const dbUser = `${svc.expectedUserPrefix}_r${Date.now().toString(36).slice(-8)}`;
  const dbPass = generatePassword();
  await provisionDbUser(dbUser, dbPass, svc.schema);
  try {
    await updateDoppler(id, {
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
  id: SecretServiceId,
  dbUser: string,
  dbPass: string
): Promise<{ dbUser: string }> {
  const svc = services[id];
  assertDbUser(dbUser);
  assertPassword(dbPass);
  if (dbUser === adminDbUser()) throw new Error('refusing to manage the admin database user');
  await provisionDbUser(dbUser, dbPass, svc.schema);
  await updateDoppler(id, {
    DB_USER: dbUser,
    DB_PASS: dbPass,
    DB_SCHEMA: svc.schema,
    DB_AUTO_MIGRATE: 'true',
    DB_MAX_OPEN_CONNS: '4',
    DB_QUERY_CONCURRENCY: '4'
  });
  return { dbUser };
}

export async function revokeCredential(
  id: SecretServiceId,
  dbUser: string
): Promise<{ dbUser: string }> {
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
    await conn.query(
      `GRANT SELECT, INSERT, UPDATE, DELETE ON ${schemaSql(schema)}.* TO ${accountSql(dbUser)}`
    );
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
  if (!/^bagel_(users|commands|modules|transactions|notifications)$/.test(schema)) {
    throw new Error('invalid database schema');
  }
  return `\`${schema}\``;
}
