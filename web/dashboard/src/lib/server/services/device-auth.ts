// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Every subject and payload shape the Users service's device-authorization RPCs use lives in
// this one file: a subject rename there is a one-file fix. Confirmed against the Users agent's
// scratchpad note (bagel.rpc.dashboard.device.*) and internal/domain/rpc/users/device.go.
import { rpc, rpcReply } from '@bagel/kit/server/nats';
import { WRITE_TIMEOUT_MS } from '@bagel/kit/server/service';
import { SUB } from './subjects';
import type { Platform } from '../device-auth-validate';

const DEVICE_PREFIX = process.env.NATS_DASHBOARD_DEVICE_SUBJECT_PREFIX || `${SUB.dashboard}.device`;

const DEVICE = {
  createCode: `${DEVICE_PREFIX}.create_code`,
  exchange: `${DEVICE_PREFIX}.exchange`,
  refresh: `${DEVICE_PREFIX}.refresh`,
  list: `${DEVICE_PREFIX}.list`,
  revoke: `${DEVICE_PREFIX}.revoke`,
  revokeAll: `${DEVICE_PREFIX}.revoke_all`
} as const;

export type DeviceTokens = {
  accessToken: string;
  tokenType: 'Bearer';
  expiresIn: number;
  refreshToken: string;
  grantId: string;
  deviceId: string;
};

export type DeviceGrantView = {
  grantId: string;
  deviceId: string;
  deviceName: string;
  platform: Platform;
  createdAt: string;
  lastUsedAt: string | null;
  expiresAt: string;
};

// Users' exchange/refresh reply carries a domain-specific oauth_error alongside the shared
// Refusal shape; a refusal with no oauth_error (signing key missing) is a server-side outage,
// never a reason to tell the app its code or refresh token is bad.
export type TokenOauthError = 'invalid_grant' | 'invalid_request' | 'server_error';
export type TokenOutcome = { ok: true; tokens: DeviceTokens } | { ok: false; oauthError: TokenOauthError };

type DeviceTokenReply = {
  access_token?: string;
  token_type?: string;
  expires_in?: number;
  refresh_token?: string;
  grant_id?: string;
  device_id?: string;
  oauth_error?: 'invalid_grant' | 'invalid_request';
  error?: string;
  code?: string;
};

function tokensFromReply(r: DeviceTokenReply): DeviceTokens {
  return {
    accessToken: r.access_token as string,
    tokenType: 'Bearer',
    expiresIn: r.expires_in as number,
    refreshToken: r.refresh_token as string,
    grantId: r.grant_id as string,
    deviceId: r.device_id as string
  };
}

function outcomeFromReply(r: DeviceTokenReply): TokenOutcome {
  if (r.access_token) return { ok: true, tokens: tokensFromReply(r) };
  if (r.oauth_error === 'invalid_grant' || r.oauth_error === 'invalid_request') {
    return { ok: false, oauthError: r.oauth_error };
  }
  return { ok: false, oauthError: 'server_error' };
}

async function callForTokens(subject: string, payload: unknown): Promise<TokenOutcome> {
  try {
    return outcomeFromReply(await rpcReply<DeviceTokenReply>(subject, payload, WRITE_TIMEOUT_MS));
  } catch {
    // Transport failure (timeout, no responder): transient, never a coded refusal.
    return { ok: false, oauthError: 'server_error' };
  }
}

export async function deviceCodeCreate(args: {
  userId: string;
  codeChallenge: string;
  redirectUri: string;
  deviceName: string;
  platform: Platform;
}): Promise<string> {
  const r = await rpc<{ code: string }>(
    DEVICE.createCode,
    {
      user_id: args.userId,
      code_challenge: args.codeChallenge,
      redirect_uri: args.redirectUri,
      device_name: args.deviceName,
      platform: args.platform
    },
    WRITE_TIMEOUT_MS
  );
  return r.code;
}

export function deviceCodeExchange(args: { code: string; codeVerifier: string; redirectUri: string }): Promise<TokenOutcome> {
  return callForTokens(DEVICE.exchange, { code: args.code, code_verifier: args.codeVerifier, redirect_uri: args.redirectUri });
}

export function deviceTokenRefresh(refreshToken: string): Promise<TokenOutcome> {
  return callForTokens(DEVICE.refresh, { refresh_token: refreshToken });
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
