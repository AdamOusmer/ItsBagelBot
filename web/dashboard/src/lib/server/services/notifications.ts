// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { POLICY } from '@bagel/kit/server/cache-keys';
import { defineRead, defineWrite, READ_TIMEOUT_MS } from '@bagel/kit/server/service';
import { SUB } from './subjects';
import { fabric, invalidate } from './fabric';

export type NotificationWire = {
  id: number;
  scope: 'broadcast' | 'direct';
  title: string;
  body: string;
  level: 'info' | 'success' | 'warning' | 'critical';
  created_by_login: string;
  created_at: string;
  expires_at?: string;
  read: boolean;
};

export type NotificationsForUser = {
  notifications: NotificationWire[];
  unreadCount: number;
};

function earliestExpiryMs(list: NotificationWire[]): number {
  let at = Number.POSITIVE_INFINITY;
  for (const n of list) {
    const expires = n.expires_at ? Date.parse(n.expires_at) : Number.NaN;
    if (expires < at) at = expires;
  }
  return at;
}

const notificationsList = defineRead({
  subject: `${SUB.notifications}.list`,
  request: (userId: string) => ({ user_id: userId }),
  map: (r: { notifications?: NotificationWire[]; unread_count?: number }) => {
    const notifications = r.notifications ?? [];
    return { view: { notifications, unreadCount: r.unread_count ?? 0 }, staleAtMs: earliestExpiryMs(notifications) };
  },
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (userId: string) => `notifications:${userId}`,
    policy: POLICY.entity
  }
});

export async function notificationsForUser(userId: string): Promise<NotificationsForUser> {
  const cached = await notificationsList(userId);
  if (Date.now() < cached.staleAtMs) return cached.view;
  invalidate(`notifications:${userId}`);
  return (await notificationsList(userId)).view;
}

export const notificationMarkRead = defineWrite({
  subject: `${SUB.notifications}.mark_read`,
  request: (userId: string, notificationId: number) => ({
    user_id: userId,
    notification_id: String(notificationId)
  }),
  after: (_result: unknown, userId: string) => invalidate(`notifications:${userId}`)
});

export const notificationMarkPeeked = defineWrite({
  subject: `${SUB.notifications}.mark_peeked`,
  request: (userId: string) => ({ user_id: userId }),
  after: (_result: unknown, userId: string) => invalidate(`notifications:${userId}`)
});
