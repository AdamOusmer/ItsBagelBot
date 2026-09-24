// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LayoutServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { requireAdmin } from '$lib/server/access';
import { notificationsList, type NotificationWire } from '$lib/server/services';

const BELL_PEEK = 5;
const DEMO = dev && process.env.DEMO === '1';

export const load: LayoutServerLoad = async ({ locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw redirect(302, '/login');

  const recentNotifications: Promise<NotificationWire[]> = DEMO
    ? import('$lib/server/demo-data').then(({ sampleNotifications }) =>
        sampleNotifications.slice(0, BELL_PEEK)
      )
    : notificationsList(1)
        .then((r) => r.notifications.slice(0, BELL_PEEK))
        .catch(() => []);

  return {
    id: admin.id,
    displayName: admin.display_name,
    login: admin.login,
    role: admin.role,
    recentNotifications
  };
};
