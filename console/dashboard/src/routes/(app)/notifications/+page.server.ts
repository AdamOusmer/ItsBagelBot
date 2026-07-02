import type { Actions, PageServerLoad } from './$types';
import { redirect, fail } from '@sveltejs/kit';
import { notificationsForUser, notificationMarkRead, type NotificationWire } from '$lib/server/services';
import { demoNotifications } from '$lib/server/demo-notifications';
import { env } from '$env/dynamic/private';

export const load: PageServerLoad = async ({ locals }) => {
  if (env.DEMO === '1') {
    return { notifications: demoNotifications, degraded: false };
  }

  const s = locals.session;
  // Owner-only: notifications are account-level, not part of a delegated
  // section grant.
  if (!s || s.delegate_of) throw redirect(302, '/');

  try {
    const result = await notificationsForUser(s.user_id);
    return { notifications: result.notifications, degraded: false };
  } catch {
    return { notifications: [] as NotificationWire[], degraded: true };
  }
};

export const actions: Actions = {
  markRead: async ({ request, locals }) => {
    const s = locals.session;
    if (!s) return fail(401, { error: 'Not signed in.' });
    if (s.delegate_of) return fail(403, { error: 'Only the account owner can do that.' });

    const id = Number(String((await request.formData()).get('id') ?? ''));
    if (!Number.isFinite(id) || id <= 0) return fail(400, { error: 'id required' });

    if (env.DEMO === '1') return { ok: true };

    try {
      await notificationMarkRead(s.user_id, id);
      return { ok: true };
    } catch {
      return fail(502, { error: 'Could not update. Try again in a moment.' });
    }
  }
};
