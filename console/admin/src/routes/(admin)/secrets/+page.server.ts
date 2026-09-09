// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { auditAppend } from '$lib/server/services';
import { allows, requireRole, type AdminIdentity } from '$lib/server/access';
import { audit } from '$lib/server/audit';
import {
  credentialStatuses,
  revokeCredential,
  rotateCredential,
  scopeReport,
  serviceIds,
  serviceOf,
  setCredential,
  type DbCredentialStatus,
  type ScopeReport,
  type SecretServiceId
} from '$lib/server/secrets';

export type SecretsBundle = {
  services: DbCredentialStatus[];
  scope: ScopeReport;
};

const DEMO = dev && process.env.DEMO === '1';

async function loadBundle(): Promise<SecretsBundle> {
  const [services, scope] = await Promise.all([credentialStatuses(), scopeReport()]);
  return { services, scope };
}

// Streamed: the shell renders immediately; the two Doppler round trips
// (statuses, scope probe, in parallel) hydrate in.
export const load: PageServerLoad = async ({ parent }) => {
  const layout = await parent();
  if (!allows(layout.role, 'secrets.manage')) throw redirect(302, '/');

  const bundle: Promise<SecretsBundle> = DEMO
    ? import('$lib/server/demo-data').then(({ demoSecretsBundle }) =>
        demoSecretsBundle(serviceIds())
      )
    : loadBundle();
  return { bundle };
};

async function managerFromLocals(locals: App.Locals): Promise<AdminIdentity | null> {
  return requireRole({ locals }, 'secrets.manage');
}

function serviceFromForm(f: FormData): SecretServiceId {
  const service = serviceOf(String(f.get('service') ?? ''));
  if (!service) throw new Error('invalid service');
  return service;
}

// secretAction wraps the shared shape of every mutation here: manager gate,
// service parse, type-to-confirm phrase check, demo short-circuit, the write,
// and the audit trail.
type SecretActionName = 'db_credential_rotate' | 'db_credential_set' | 'db_credential_revoke';

type SecretSpec = {
  name: SecretActionName; // audit action id
  confirm: (service: SecretServiceId, f: FormData) => string;
  run: (
    service: SecretServiceId,
    f: FormData
  ) => Promise<{ notice: string; target?: string }>;
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
      return { action: { ok: true, notice: out.notice } };
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
  })
};
