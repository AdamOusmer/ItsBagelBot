// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc, publish } from '@bagel/kit/server/nats';
import { SUB } from './subjects';

// Unenroll is flushed: account deletion relies on it reaching NATS before the delete RPC.
export async function publishEventSub(broadcasterId: string, enabled: boolean): Promise<void> {
  await publish(
    SUB.outgress,
    {
      type: 'eventsub',
      broadcaster_id: broadcasterId,
      payload: { enabled }
    },
    { flush: !enabled }
  );
}

async function publishEventSubMode(broadcasterId: string, mode: string): Promise<void> {
  await publish(SUB.outgress, {
    type: 'eventsub',
    broadcaster_id: broadcasterId,
    payload: { mode }
  });
}

export function publishEventSubReconnect(broadcasterId: string): Promise<void> {
  return publishEventSubMode(broadcasterId, 'reconnect');
}

export function publishEventSubEnsureOptional(broadcasterId: string): Promise<void> {
  return publishEventSubMode(broadcasterId, 'ensure_optional');
}

export type ChannelSubState = {
  state: 'ok' | 'pending' | 'failing' | 'revoked' | 'chat_banned' | 'unenrolled' | 'unknown';
  error: string;
  checkedAt: string | null;
  verification?: SubVerification;
};

export type SubVerification = {
  expected: number;
  present: number;
  missing?: string[];
  repairing: boolean;
  throttled?: boolean;
  error?: string;
};

export type SubStateOptions = { verify?: boolean; repair?: boolean };

const SUB_READ_TIMEOUT_MS = 2000;
const SUB_VERIFY_TIMEOUT_MS = 8000;

function unknownSubState(): ChannelSubState {
  return { state: 'unknown', error: '', checkedAt: null };
}

// Missing and unrecognized subscription observations remain unknown.
const KNOWN_SUB_STATES = ['ok', 'pending', 'failing', 'revoked', 'chat_banned'] as const;

function isKnownSubState(s: string): s is (typeof KNOWN_SUB_STATES)[number] {
  return (KNOWN_SUB_STATES as readonly string[]).includes(s);
}

type SubStateReply = {
  found: boolean;
  channel?: { sub_state: string; sub_error: string; sub_checked_at: string };
  verification?: SubVerification;
};

function subStateFlags(opts: SubStateOptions): { verify?: true; repair?: true } {
  return {
    ...(opts.verify ? { verify: true } : {}),
    ...(opts.repair ? { repair: true } : {})
  };
}

function subStateFromReply(r: SubStateReply): ChannelSubState {
  const verification = r.verification ? { verification: r.verification } : {};
  const c = r.found ? r.channel : undefined;
  if (!c || !isKnownSubState(c.sub_state)) {
    return { ...unknownSubState(), ...verification };
  }
  return {
    state: c.sub_state,
    error: c.sub_error || '',
    checkedAt: c.sub_checked_at || null,
    ...verification
  };
}

export async function channelSubState(
  broadcasterId: string,
  opts: SubStateOptions = {}
): Promise<ChannelSubState> {
  try {
    const r = await rpc<SubStateReply>(
      `${SUB.outgressRpc}.channel.get`,
      { broadcaster_id: broadcasterId, ...subStateFlags(opts) },
      opts.verify ? SUB_VERIFY_TIMEOUT_MS : SUB_READ_TIMEOUT_MS
    );
    return subStateFromReply(r);
  } catch {
    return unknownSubState();
  }
}
