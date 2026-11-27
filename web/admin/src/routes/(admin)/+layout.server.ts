// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LayoutServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { requireAdmin } from '#lib/server/access';
import { liveUserCount } from '#lib/server/live-count';
import { notificationsList, type NotificationWire } from '#lib/server/services';

const BELL_PEEK = 5;

export const load: LayoutServerLoad = async ({ locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw redirect(302, '/login');

  const recentNotifications: Promise<NotificationWire[]> = notificationsList(1)
    .then((r) => r.notifications.slice(0, BELL_PEEK))
    .catch(() => []);

  return {
    id: admin.id,
    displayName: admin.display_name,
    login: admin.login,
    role: admin.role,
    recentNotifications,
    liveCount: liveUserCount(admin.id)
  };
};
