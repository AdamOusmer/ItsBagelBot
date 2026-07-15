import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { auditAppend } from '$lib/server/services';
import { isManager, requireAdmin, type AdminIdentity } from '$lib/server/access';
import {
  credentialStatuses,
  listServiceTokens,
  mintServiceToken,
  revokeServiceToken,
  revokeCredential,
  rotateCredential,
  scopeReport,
  serviceIds,
  serviceOf,
  setCredential,
  type DbCredentialStatus,
  type ScopeReport,
  type SecretServiceId,
  type ServiceTokenView
} from '$lib/server/secrets';

export type SecretsBundle = {
  services: DbCredentialStatus[];
  tokens: Record<string, ServiceTokenView[]>;
  scope: ScopeReport;
};

const DEMO = dev && process.env.DEMO === '1';

async function loadBundle(): Promise<SecretsBundle> {
  const [services, scope, tokenLists] = await Promise.all([
    credentialStatuses(),
    scopeReport(),
    Promise.all(
      serviceIds().map(async (id) => {
        try {
          return [id, await listServiceTokens(id)] as const;
        } catch {
          return [id, []] as const;
        }
      })
    )
  ]);
  return { services, scope, tokens: Object.fromEntries(tokenLists) };
}

// Streamed: the shell renders immediately; the Doppler round trips (statuses,
// scope probe, token lists — all parallel) hydrate in.
export const load: PageServerLoad = async ({ parent }) => {
  const layout = await parent();
  if (!isManager(layout.role)) throw redirect(302, '/');

  const bundle: Promise<SecretsBundle> = DEMO
    ? import('$lib/server/demo-data').then(({ demoSecretsBundle }) =>
        demoSecretsBundle(serviceIds())
      )
    : loadBundle();
  return { bundle };
};

type AuditOutcome = {
  action: string;
  target: string;
  ok: boolean;
  error?: string;
};

// audit records a mutating action best-effort: a logging failure must never
// block the operator action it describes. Skipped in demo.
function audit(admin: AdminIdentity, outcome: AuditOutcome): void {
  if (DEMO) return;
  auditAppend({
    actor_id: admin.id,
    actor_login: admin.login,
    action: outcome.action,
    target: outcome.target,
    detail: '',
    ok: outcome.ok,
    error: outcome.error ?? ''
  }).catch(() => {});
}

async function managerFromLocals(locals: App.Locals): Promise<AdminIdentity | null> {
  const admin = await requireAdmin(locals.session);
  if (!admin || !isManager(admin.role)) return null;
  return admin;
}

function serviceFromForm(f: FormData): SecretServiceId {
  const service = serviceOf(String(f.get('service') ?? ''));
  if (!service) throw new Error('invalid service');
  return service;
}

// secretAction wraps the shared shape of every mutation here: manager gate,
// service parse, type-to-confirm phrase check, demo short-circuit, the write,
// and the audit trail.
type SecretActionName =
  | 'db_credential_rotate'
  | 'db_credential_set'
  | 'db_credential_revoke'
  | 'doppler_token_mint'
  | 'doppler_token_revoke';

type SecretSpec = {
  name: SecretActionName; // audit action id
  confirm: (service: SecretServiceId, f: FormData) => string;
  run: (
    service: SecretServiceId,
    f: FormData
  ) => Promise<{ notice: string; mintedKey?: string; target?: string }>;
};

function secretAction(spec: SecretSpec) {
  return async ({ request, locals }: { request: Request; locals: App.Locals }) => {
    const admin = await managerFromLocals(locals);
    if (!admin) return fail(403, { error: 'forbidden' });

    const f = await request.formData();
    let service: SecretServiceId | undefined;
    try {
      service = serviceFromForm(f);
      const phrase = spec.confirm(service, f);
      if (String(f.get('confirm') ?? '').trim() !== phrase) {
        return fail(400, { error: `type "${phrase}" to confirm` });
      }
      if (DEMO) {
        const { demoSecretNotice } = await import('$lib/server/demo-data');
        return { action: { ok: true, notice: demoSecretNotice(spec.name) } };
      }

      const out = await spec.run(service, f);
      audit(admin, { action: spec.name, target: `${service}:${out.target ?? ''}`, ok: true });
      return {
        action: { ok: true, notice: out.notice },
        ...(out.mintedKey ? { mintedKey: out.mintedKey } : {})
      };
    } catch (e) {
      const message = (e as Error).message;
      audit(admin, { action: spec.name, target: String(service ?? ''), ok: false, error: message });
      return fail(400, { error: message });
    }
  };
}

export const actions: Actions = {
  rotate: secretAction({
    name: 'db_credential_rotate',
    confirm: (s) => `rotate ${s}`,
    run: async (service) => {
      const result = await rotateCredential(service);
      return { notice: `${service} credential rotated to ${result.dbUser}`, target: result.dbUser };
    }
  }),

  set: secretAction({
    name: 'db_credential_set',
    confirm: (s) => `set ${s}`,
    run: async (service, f) => {
      const result = await setCredential(service, {
        dbUser: String(f.get('db_user') ?? '').trim(),
        dbPass: String(f.get('db_pass') ?? '')
      });
      return { notice: `${service} credential set to ${result.dbUser}`, target: result.dbUser };
    }
  }),

  revoke: secretAction({
    name: 'db_credential_revoke',
    confirm: (_s, f) => `revoke ${String(f.get('db_user') ?? '').trim()}`,
    run: async (service, f) => {
      const result = await revokeCredential(service, {
        dbUser: String(f.get('db_user') ?? '').trim()
      });
      return { notice: `${result.dbUser} revoked`, target: result.dbUser };
    }
  }),

  mintToken: secretAction({
    name: 'doppler_token_mint',
    confirm: (s) => `mint ${s}`,
    run: async (service, f) => {
      const expireDays = Number(f.get('expire_days') ?? '0');
      const result = await mintServiceToken(service, {
        name: String(f.get('name') ?? '').trim(),
        expireDays: Number.isFinite(expireDays) ? expireDays : 0
      });
      return {
        notice: `read-only token "${result.token.name}" minted for ${service}/prd`,
        mintedKey: result.key,
        target: result.token.name
      };
    }
  }),

  revokeToken: secretAction({
    name: 'doppler_token_revoke',
    confirm: (s) => `revoke ${s} token`,
    run: async (service, f) => {
      const slug = String(f.get('slug') ?? '').trim();
      await revokeServiceToken(service, { slug });
      return { notice: 'service token revoked', target: slug };
    }
  })
};
