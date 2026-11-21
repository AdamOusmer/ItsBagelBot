// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { allows, requireRole } from '#lib/server/access';
import { audit } from '#lib/server/audit';
import { actionError, adminText } from '#lib/server/admin-action';
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
} from '#lib/server/secrets';

export type SecretsBundle = {
  services: DbCredentialStatus[];
  scope: ScopeReport;
};

const DEMO = dev && process.env.DEMO === '1';

async function loadBundle(): Promise<SecretsBundle> {
  const [services, scope] = await Promise.all([credentialStatuses(), scopeReport()]);
  return { services, scope };
}

export const load: PageServerLoad = async ({ parent }) => {
  const layout = await parent();
  if (!allows(layout.role, 'secrets.manage')) throw redirect(302, '/');

  const bundle: Promise<SecretsBundle> = DEMO
    ? import('#lib/server/demo-data').then(({ demoSecretsBundle }) =>
        demoSecretsBundle(serviceIds())
      )
    : loadBundle();
  return { bundle };
};

function serviceFromForm(form: FormData): SecretServiceId {
  const service = serviceOf(String(form.get('service') ?? ''));
  if (!service) throw new Error('invalid service');
  return service;
}

type SecretActionName = 'db_credential_rotate' | 'db_credential_set' | 'db_credential_revoke';

type SecretSpec = {
  name: SecretActionName;
  confirm: (service: SecretServiceId, form: FormData) => string;
  run: (service: SecretServiceId, form: FormData) => Promise<{ dbUser: string }>;
};

function secretAction(spec: SecretSpec) {
  return async ({ request, locals }: { request: Request; locals: App.Locals }) => {
    const admin = await requireRole({ locals }, 'secrets.manage');
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });

    const form = await request.formData();
    let service: SecretServiceId | undefined;
    try {
      service = serviceFromForm(form);
      const phrase = spec.confirm(service, form);
      if (String(form.get('confirm') ?? '').trim() !== phrase) {
        return fail(400, { error: adminText(locals.locale, 'admin.secrets.typeToConfirm', { phrase }) });
      }
      if (DEMO) {
        return { action: { ok: true, notice: adminText(locals.locale, 'admin.secrets.demoNotice', { action: spec.name }) } };
      }

      const { dbUser } = await spec.run(service, form);
      audit(admin, { action: spec.name, target: `${service}:${dbUser}`, ok: true });
      return { action: { ok: true, notice: adminText(locals.locale, `admin.secrets.${spec.name}`, { service, dbUser }) } };
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
    confirm: (service) => `rotate ${service}`,
    run: rotateCredential
  }),

  set: secretAction({
    name: 'db_credential_set',
    confirm: (service) => `set ${service}`,
    run: (service, form) => setCredential(service, {
      dbUser: String(form.get('db_user') ?? '').trim(),
      dbPass: String(form.get('db_pass') ?? '')
    })
  }),

  revoke: secretAction({
    name: 'db_credential_revoke',
    confirm: (_service, form) => `revoke ${String(form.get('db_user') ?? '').trim()}`,
    run: (service, form) => revokeCredential(service, {
      dbUser: String(form.get('db_user') ?? '').trim()
    })
  })
};
