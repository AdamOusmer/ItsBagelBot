// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { giveawayDefaults, type EligibleCount, type GiveawayRound, type GiveawayWinner } from '#lib/channel-giveaway';
import { MOD, moduleDef } from '@bagel/kit';
import { logger } from '@bagel/kit/server/logger';
import { rpc, rpcRefusal, rpcReply, type RpcError } from '@bagel/kit/server/nats';
import { listModulesForEditing } from './commands-store';
import { readModuleBlob } from './module-blob';
import { SUB } from './services/subjects';
import { readGiveawayConfig } from './giveaway-input';

type UserId = string;
type PresetName = string;

type DrawRead = 'live' | 'eligible_count';
export type DrawMutation = 'start' | 'close' | 'extend' | 'cancel' | 'reroll' | 'end_slots' | 'skip_rule' | 'mark_delivered';

export interface DrawInput {
  preset?: PresetName;
  prize?: string;
  winners?: number;
  window_seconds?: number;
  round_id?: string;
  slot?: number;
  delivered?: boolean;
}

export interface DrawActor {
  userId: UserId;
  actorId: UserId;
}

interface DrawReply extends Partial<EligibleCount> {
  rounds?: GiveawayRound[];
  round?: GiveawayRound;
  done?: boolean;
  affected?: number;
  reason?: string;
}

export interface LiveGiveaways {
  rounds: GiveawayRound[];
  degraded: boolean;
}

export const giveawayDef = moduleDef(MOD.giveaway)!;
export const unavailableGiveaways: LiveGiveaways = { rounds: [], degraded: true };
export const emptyGiveawaySettings = { enabled: false, revision: 0, config: giveawayDefaults, degraded: true };

const drawSubject = (verb: DrawRead | DrawMutation) => `rpc.draw.giveaway.${verb}`;
const drawIdentity = (userId: UserId) => ({ user_id: userId, source: 'dashboard' });
const DRAW_TIMEOUT_MS = 10_000;

export async function liveGiveaways(userId: UserId): Promise<LiveGiveaways> {
  try {
    const { rounds } = await rpc<DrawReply>(drawSubject('live'), drawIdentity(userId), DRAW_TIMEOUT_MS);
    return { rounds: rounds ?? [], degraded: false };
  } catch (err) {
    logger.warn({ err }, '[giveaways] live unavailable');
    return unavailableGiveaways;
  }
}

export async function eligibleCount(userId: UserId, preset?: PresetName): Promise<EligibleCount | null> {
  const reply = await rpc<DrawReply>(drawSubject('eligible_count'), { ...drawIdentity(userId), preset }, DRAW_TIMEOUT_MS);
  return reply.count === undefined ? null : { count: reply.count };
}

export class DrawRefusal extends Error {
  constructor(readonly refusal: RpcError, readonly reason?: string) {
    super(refusal.message);
  }
}

export async function mutateGiveaway(verb: DrawMutation, actor: DrawActor, input: DrawInput) {
  const reply = await rpcReply<DrawReply>(drawSubject(verb), {
    ...drawIdentity(actor.userId),
    actor_id: actor.actorId,
    ...input
  }, DRAW_TIMEOUT_MS);
  const refusal = rpcRefusal(reply);
  if (refusal) throw new DrawRefusal(refusal, reply.reason);
  if (!reply.done) throw new Error('Giveaway mutation returned no completion');
  return reply;
}

export async function pastWinners(userId: UserId, limit: number, offset = 0): Promise<GiveawayWinner[]> {
  const query = { user_id: userId, limit, ...(offset ? { offset } : {}) };
  const { winners } = await rpc<{ winners?: GiveawayWinner[] }>(`${SUB.modules}.giveaway.recent`, query, 2000);
  return winners ?? [];
}

export async function publicGiveawayWinners(userId: UserId) {
  try {
    const { enabled } = await readModuleBlob(userId, MOD.giveaway);
    if (!enabled) return { winners: [], degraded: false };
    const winners = await pastWinners(userId, 10);
    return {
      winners: winners.map(({ round_id, slot, login, prize, won_at }) => ({ round_id, slot, login, prize, won_at })),
      degraded: false
    };
  } catch {
    return { winners: [], degraded: true };
  }
}

export async function giveawaySettings(userId: UserId) {
  try {
    const rows = await listModulesForEditing(userId);
    const row = rows.find((candidate) => candidate.name === MOD.giveaway);
    return {
      enabled: row?.is_enabled ?? false,
      revision: row?.revision ?? 0,
      config: readGiveawayConfig(row?.configs),
      degraded: false
    };
  } catch (err) {
    logger.warn({ err }, '[giveaways] settings unavailable');
    return emptyGiveawaySettings;
  }
}
