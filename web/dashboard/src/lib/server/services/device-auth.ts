// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Dashboard approves new devices and manages grants through Users RPC. Native
// code exchange and refresh belong to the independent overlay auth service.
import { rpc } from '@bagel/kit/server/nats';
import { WRITE_TIMEOUT_MS } from '@bagel/kit/server/service';
import { SUB } from './subjects';
import type { Platform } from '../device-auth-validate';

const DEVICE_PREFIX = process.env.NATS_DASHBOARD_DEVICE_SUBJECT_PREFIX || `${SUB.dashboard}.device`;

const DEVICE = {
  createCode: `${DEVICE_PREFIX}.create_code`,
  list: `${DEVICE_PREFIX}.list`,
  revoke: `${DEVICE_PREFIX}.revoke`,
  revokeAll: `${DEVICE_PREFIX}.revoke_all`
} as const;

export type DeviceGrantView = {
  grantId: string;
  deviceId: string;
  deviceName: string;
  platform: Platform;
  createdAt: string;
  lastUsedAt: string | null;
  expiresAt: string;
};

export async function deviceCodeCreate(args: {
  userId: string;
  codeChallenge: string;
  dpopJkt: string;
  redirectUri: string;
  deviceName: string;
  platform: Platform;
}): Promise<string> {
  const r = await rpc<{ code: string }>(
    DEVICE.createCode,
    {
      user_id: args.userId,
      code_challenge: args.codeChallenge,
      dpop_jkt: args.dpopJkt,
      redirect_uri: args.redirectUri,
      device_name: args.deviceName,
      platform: args.platform
    },
    WRITE_TIMEOUT_MS
  );
  return r.code;
}

export async function deviceGrantList(userId: string): Promise<DeviceGrantView[]> {
  const r = await rpc<{
    grants?: {
      grant_id: string;
      device_id: string;
      device_name: string;
      platform: Platform;
      created_at: string;
      last_used_at?: string | null;
      expires_at: string;
    }[];
  }>(DEVICE.list, { user_id: userId }, WRITE_TIMEOUT_MS);
  return (r.grants ?? []).map((g) => ({
    grantId: g.grant_id,
    deviceId: g.device_id,
    deviceName: g.device_name,
    platform: g.platform,
    createdAt: g.created_at,
    lastUsedAt: g.last_used_at ?? null,
    expiresAt: g.expires_at
  }));
}

export async function deviceGrantRevoke(args: { userId: string; grantId: string }): Promise<void> {
  await rpc(DEVICE.revoke, { user_id: args.userId, grant_id: args.grantId }, WRITE_TIMEOUT_MS);
}

export async function deviceGrantRevokeAll(userId: string, reason = 'user'): Promise<void> {
  await rpc(DEVICE.revokeAll, { user_id: userId, reason }, WRITE_TIMEOUT_MS);
}
