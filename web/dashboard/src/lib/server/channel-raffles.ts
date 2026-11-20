// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { GIVEAWAY_LIMITS as pins } from '@bagel/kit';
import { rpcRefusal, rpcReply } from '@bagel/kit/server/nats';
import { DrawRefusal, liveGiveaways, type DrawActor } from './channel-giveaways';
import { drawRoundID } from './giveaway-input';
import { raffleStatus } from './loyalty-store';

type RaffleVerb = 'start' | 'draw' | 'cancel';

interface WinnerChoice {
  winners: number;
  auto: boolean;
}

type RaffleMutation =
  | { verb: 'start'; input: WinnerChoice & { amount: number; seconds: number; split: boolean } }
  | { verb: 'draw'; input: WinnerChoice & { round_id: string } }
  | { verb: 'cancel'; input: { round_id: string } };

interface RaffleReply extends Partial<WinnerChoice> {
  round_id?: string;
  closes_ms?: number;
  amount?: number;
  done?: boolean;
  reason?: string;
}

export const emptyLiveRaffles = { rounds: [], unpaid: [], degraded: false };

async function liveRaffles(userId: DrawActor['userId']) {
  try {
    const { rounds, unpaid } = await raffleStatus(userId);
    return { rounds: (rounds ?? []).map((round) => ({ ...round, kind: 'raffle' as const })), unpaid: unpaid ?? [], degraded: false };
  } catch {
    return { ...emptyLiveRaffles, degraded: true };
  }
}

export async function liveDraws(userId: DrawActor['userId']) {
  const [giveaways, raffles] = await Promise.all([liveGiveaways(userId), liveRaffles(userId)]);
  return { ...giveaways, raffles };
}

export type LiveDraws = Awaited<ReturnType<typeof liveDraws>>;

function formInteger(form: FormData, key: 'amount' | 'seconds' | 'winners', max: number) {
  const raw = form.get(key) ?? '';
  if (typeof raw !== 'string' || !/^\d*$/.test(raw)) return null;
  const value = Number(raw);
  return Number.isSafeInteger(value) && value <= max ? value : null;
}

function winnerChoice(form: FormData): WinnerChoice | null {
  const winners = formInteger(form, 'winners', pins.maxWinners);
  const auto = form.get('auto');
  if (winners === null || (auto !== null && auto !== 'on')) return null;
  return { winners: auto ? 0 : winners, auto: auto === 'on' };
}

function raffleStart(form: FormData): RaffleMutation | null {
  const choice = winnerChoice(form);
  const amount = formInteger(form, 'amount', pins.raffleMaxPrize);
  const seconds = formInteger(form, 'seconds', pins.maxOpen);
  const mode = form.get('mode');
  if (!choice || amount === null || seconds === null) return null;
  if (seconds > 0 && seconds < pins.raffleMinSeconds) return null;
  if (mode !== 'single' && mode !== 'split') return null;
  const split = mode === 'split';
  const winners = split ? choice : { winners: 0, auto: false };
  return { verb: 'start', input: { amount, seconds, split, ...winners } };
}

export function raffleInput(verb: RaffleVerb, form: FormData): RaffleMutation | null {
  if (verb === 'start') return raffleStart(form);
  const round_id = drawRoundID(form);
  if (!round_id) return null;
  if (verb === 'cancel') return { verb, input: { round_id } };
  const winners = winnerChoice(form);
  return winners ? { verb, input: { round_id, ...winners } } : null;
}

export async function mutateRaffle(actor: DrawActor, { verb, input }: RaffleMutation) {
  const reply = await rpcReply<RaffleReply>(`rpc.draw.raffle.${verb}`, {
    user_id: actor.userId, actor_id: actor.actorId, ...input
  }, 10_000);
  const refusal = rpcRefusal(reply);
  if (refusal) throw new DrawRefusal(refusal, reply.reason);
  const applied = verb === 'start' ? !!reply.round_id : reply.done === true;
  if (!applied) throw new Error('Raffle mutation returned no completion');
  return reply;
}
