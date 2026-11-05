// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createHash, randomBytes, timingSafeEqual } from 'node:crypto';
import { logger } from '@bagel/kit/server/logger';
import { masterClient } from '@bagel/kit/server/valkey-master';
import { listModulesForEditing, upsertModule } from './commands-store';
import { OVERLAY_REVOKE_MESSAGE, overlayChannel } from './overlay-wire';
import { valkeyGuard } from './valkey-guard';

const OVERLAY_MODULE = 'overlay';
const SECRET_BYTES = 32;
const TOKEN_SHAPE = /^(\d{1,20})\.[\w-]{43}$/;
const HASH_SHAPE = /^[0-9a-f]{64}$/;
const DECOY_HASH = sha256(randomBytes(SECRET_BYTES));

type ChannelId = string;
type OverlayToken = string;
type TokenHash = string;

export interface OverlayTokenStore {
  readHash(channelId: ChannelId): Promise<TokenHash | null>;
  writeHash(channelId: ChannelId, hash: TokenHash): Promise<void>;
  announceRevoked(channelId: ChannelId): Promise<void>;
}

export interface VerifiedOverlay {
  channelId: ChannelId;
  tokenHash: TokenHash;
}

function sha256(input: OverlayToken | Uint8Array): TokenHash {
  return createHash('sha256').update(input).digest('hex');
}

function sameHash(left: TokenHash, right: TokenHash): boolean {
  return timingSafeEqual(Buffer.from(left, 'hex'), Buffer.from(right, 'hex'));
}

export function hashOverlayToken(token: OverlayToken): TokenHash {
  return sha256(token);
}

// A channel with no token still runs the compare, so a miss looks like a wrong token.
export function overlayHashMatches(tokenHash: TokenHash, stored: TokenHash | null): boolean {
  const usable = stored !== null && HASH_SHAPE.test(stored);
  const matched = sameHash(tokenHash, usable ? stored : DECOY_HASH);
  return usable && matched;
}

export async function verifyOverlayToken(token: OverlayToken, store: OverlayTokenStore): Promise<VerifiedOverlay | null> {
  const channelId = TOKEN_SHAPE.exec(token)?.[1];
  if (!channelId) return null;
  const tokenHash = hashOverlayToken(token);
  return overlayHashMatches(tokenHash, await store.readHash(channelId)) ? { channelId, tokenHash } : null;
}

export async function stillValidOverlayHash(
  channelId: ChannelId,
  tokenHash: TokenHash,
  store: OverlayTokenStore
): Promise<boolean> {
  return overlayHashMatches(tokenHash, await store.readHash(channelId));
}

// Replaces the stored hash first: any stream that outlives a lost announcement fails its next recheck.
export async function rotateOverlayToken(channelId: ChannelId, store: OverlayTokenStore): Promise<OverlayToken> {
  const token = `${channelId}.${randomBytes(SECRET_BYTES).toString('base64url')}`;
  await store.writeHash(channelId, hashOverlayToken(token));
  await store.announceRevoked(channelId).catch((err: Error) => {
    logger.warn({ err: err.message }, '[overlay] revoke announcement failed');
  });
  return token;
}

export async function overlayConfigured(channelId: ChannelId, store: OverlayTokenStore): Promise<boolean> {
  return (await store.readHash(channelId)) !== null;
}

const guarded = valkeyGuard('valkey-overlay-revoke', 'valkey-overlay-revoke');

function storedHash(configs: unknown): TokenHash | null {
  const hash = (configs as { token_hash?: unknown } | null | undefined)?.token_hash;
  return typeof hash === 'string' ? hash : null;
}

// Reads the owning service: a stale projection could keep a rotated token alive.
export const moduleTokenStore: OverlayTokenStore = {
  async readHash(channelId) {
    const rows = await listModulesForEditing(channelId);
    return storedHash(rows.find((row) => row.name === OVERLAY_MODULE)?.configs);
  },
  async writeHash(channelId, hash) {
    await upsertModule(channelId, OVERLAY_MODULE, false, { token_hash: hash });
  },
  async announceRevoked(channelId) {
    const client = masterClient();
    if (!client) return;
    await guarded(() => client.publish(overlayChannel(channelId), OVERLAY_REVOKE_MESSAGE));
  }
};
