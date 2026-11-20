// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { GIVEAWAY_LIMITS as pins } from '@bagel/kit';
import { rpcRefusal, rpcReply } from '@bagel/kit/server/nats';
import { DrawRefusal, liveGiveaways, type DrawActor } from './channel-giveaways';
import { drawRoundID } from './giveaway-input';
import { raffleStatus } from './loyalty-store';

type RaffleVerb = 'start' | 'draw' | 'cancel';

const integerLimits = {
  amount: { min: 0, max: pins.raffleMaxPrize },
  seconds: { min: pins.raffleMinSeconds, max: pins.maxOpen },
  winners: { min: 0, max: pins.maxWinners }
};
const splitModes = new Map<FormDataEntryValue | null, boolean>([['single', false], ['split', true]]);

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

function formInteger(form: FormData, key: keyof typeof integerLimits) {
  const raw = form.get(key) ?? '';
  if (typeof raw !== 'string' || !/^\d*$/.test(raw)) return null;
  const value = Number(raw);
  if (value === 0) return value;
  if (!Number.isSafeInteger(value)) return null;
  const { min, max } = integerLimits[key];
  return value < min || value > max ? null : value;
}

function winnerChoice(form: FormData): WinnerChoice | null {
  const winners = formInteger(form, 'winners');
  if (winners === null) return null;
  const auto = form.get('auto');
  if (auto === null) return { winners, auto: false };
  return auto === 'on' ? { winners: 0, auto: true } : null;
}

function rafflePrize(form: FormData) {
  const amount = formInteger(form, 'amount');
  const seconds = formInteger(form, 'seconds');
  return amount === null || seconds === null ? null : { amount, seconds };
}

function raffleStart(form: FormData): RaffleMutation | null {
  const choice = winnerChoice(form);
  const prize = rafflePrize(form);
  if (!choice || !prize) return null;
  const split = splitModes.get(form.get('mode'));
  if (split === undefined) return null;
  const winners = split ? choice : { winners: 0, auto: false };
  return { verb: 'start', input: { ...prize, split, ...winners } };
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
