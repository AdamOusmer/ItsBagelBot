import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { auditAppend } from '$lib/server/services';
import { isManager, requireAdmin, type AdminIdentity } from '$lib/server/access';
import {
  credentialStatuses,
  revokeCredential,
  rotateCredential,
  serviceOf,
  setCredential
} from '$lib/server/db-credentials';

export const load: PageServerLoad = async ({ parent }) => {
  const layout = await parent();
  const admin: AdminIdentity = {
    id: layout.id,
    login: layout.login,
    display_name: layout.displayName,
    role: layout.role
  };
  if (!isManager(admin.role)) throw redirect(302, '/');

  return {
    me: admin,
    services: await credentialStatuses()
  };
};

function audit(admin: AdminIdentity, action: string, target: string, detail: string, ok: boolean, error = ''): void {
  auditAppend({
    actor_id: admin.id,
    actor_login: admin.login,
    action,
    target,
    detail,
    ok,
    error
  }).catch(() => {});
}

function serviceFromForm(f: FormData) {
  const service = serviceOf(String(f.get('service') ?? ''));
  if (!service) throw new Error('invalid service');
  return service;
}

export const actions: Actions = {
  rotate: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin || !isManager(admin.role)) return fail(403, { error: 'forbidden' });

    const f = await request.formData();
    let service;
    try {
      service = serviceFromForm(f);
      const confirm = String(f.get('confirm') ?? '').trim();
      if (confirm !== `rotate ${service}`) return fail(400, { error: `type "rotate ${service}" to confirm` });

      const result = await rotateCredential(service);
      audit(admin, 'db_credential_rotate', service, result.dbUser, true);
      return { ok: true, notice: `${service} credential rotated to ${result.dbUser}` };
    } catch (e) {
      const message = (e as Error).message;
      audit(admin, 'db_credential_rotate', String(service ?? ''), '', false, message);
      return fail(400, { error: message });
    }
  },

  set: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin || !isManager(admin.role)) return fail(403, { error: 'forbidden' });

    const f = await request.formData();
    let service;
    const dbUser = String(f.get('db_user') ?? '').trim();
    try {
      service = serviceFromForm(f);
      const dbPass = String(f.get('db_pass') ?? '');
      const confirm = String(f.get('confirm') ?? '').trim();
      if (confirm !== `set ${service}`) return fail(400, { error: `type "set ${service}" to confirm` });

      const result = await setCredential(service, dbUser, dbPass);
      audit(admin, 'db_credential_set', service, result.dbUser, true);
      return { ok: true, notice: `${service} credential set to ${result.dbUser}` };
    } catch (e) {
      const message = (e as Error).message;
      audit(admin, 'db_credential_set', String(service ?? ''), dbUser, false, message);
      return fail(400, { error: message });
    }
  },

  revoke: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin || !isManager(admin.role)) return fail(403, { error: 'forbidden' });

    const f = await request.formData();
    let service;
    const dbUser = String(f.get('db_user') ?? '').trim();
    try {
      service = serviceFromForm(f);
      const confirm = String(f.get('confirm') ?? '').trim();
      if (confirm !== `revoke ${dbUser}`) return fail(400, { error: `type "revoke ${dbUser}" to confirm` });

      const result = await revokeCredential(service, dbUser);
      audit(admin, 'db_credential_revoke', service, result.dbUser, true);
      return { ok: true, notice: `${result.dbUser} revoked` };
    } catch (e) {
      const message = (e as Error).message;
      audit(admin, 'db_credential_revoke', String(service ?? ''), dbUser, false, message);
      return fail(400, { error: message });
    }
  }
};
