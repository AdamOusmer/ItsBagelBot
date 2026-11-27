// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { SUB } from '#lib/server/services/subjects';
import {
  DEMO_BOARD_ID,
  demoAccountState,
  demoAuthorizedDashboards,
  demoDelegationGiven,
  demoDeviceGrants,
  demoNotifications,
  demoSession
} from '../fixtures';
import type { Routes } from './types';

function stateReply() {
  const a = demoAccountState;
  return {
    active: a.active,
    status: a.status,
    onboarded: a.onboarded,
    alpha: a.alpha,
    terms_version: a.termsVersion,
    marketing_status: a.marketingStatus,
    creator_code: a.creatorCode,
    username: a.username,
    display_name: a.displayName
  };
}

function markRead(payload: Record<string, unknown>) {
  for (const n of demoNotifications) if (String(n.id) === payload.notification_id) n.read = true;
  return { ok: true };
}

function notifications() {
  return { notifications: demoNotifications, unread_count: demoNotifications.filter((n) => !n.read).length };
}

function delegationAccess() {
  const grants = demoAuthorizedDashboards.map((d) => ({
    owner_user_id: new URL(d.href, 'http://x').searchParams.get('owner') ?? '',
    owner_login: d.name,
    sections: ['commands']
  }));
  return { grants };
}

function deviceGrants() {
  const grants = demoDeviceGrants.map((g) => ({
    grant_id: g.grantId,
    device_id: g.deviceId,
    device_name: g.deviceName,
    platform: g.platform,
    created_at: g.createdAt,
    last_used_at: g.lastUsedAt,
    expires_at: g.expiresAt
  }));
  return { grants };
}

function resolveLogin(payload: Record<string, unknown>) {
  return { user_id: DEMO_BOARD_ID, username: String(payload.login ?? ''), display_name: demoSession().display_name };
}

export const accountRoutes: Routes = {
  [`${SUB.dashboard}.state_get`]: stateReply,
  [`${SUB.dashboard}.grant_has`]: () => ({ has_grant: true }),
  [`${SUB.dashboard}.login_resolve`]: resolveLogin,
  [`${SUB.dashboard}.marketing_token_get`]: () => ({ status: 'granted', masked_email: 'd***@example.com', locale: 'en' }),
  [`${SUB.dashboard}.device.create_code`]: () => ({ code: `demo-code-${Date.now()}` }),
  [`${SUB.dashboard}.device.list`]: deviceGrants,
  [`${SUB.notifications}.list`]: notifications,
  [`${SUB.notifications}.mark_read`]: markRead,
  [`${SUB.delegation}.list`]: () => ({ grants: demoDelegationGiven }),
  [`${SUB.delegation}.access`]: delegationAccess,
  [`${SUB.outgressRpc}.channel.get`]: () => ({
    found: true,
    channel: { sub_state: 'ok', sub_error: '', sub_checked_at: new Date().toISOString() }
  })
};
