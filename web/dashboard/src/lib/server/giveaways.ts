import { rpc } from '@bagel/kit/server/nats';

const SUBJECT = 'bagel.rpc.transactions.giveaways.mine';

export type PrizeAward = {
  id: string;
  prizeMonths: number;
  state: string;
  billingState: string;
  emailState: string;
  plannedStart?: string | null;
  plannedEnd?: string | null;
  confirmedStart?: string | null;
  confirmedEnd?: string | null;
  selectedAt?: string | null;
  failureReason?: string | null;
  retryCount: number;
};

function mapAward(raw: Record<string, unknown>): PrizeAward {
  return {
    id: String(raw.id),
    prizeMonths: Number(raw.prize_months),
    state: String(raw.state),
    billingState: String(raw.billing_state),
    emailState: String(raw.email_state),
    plannedStart: raw.planned_start as string | null | undefined,
    plannedEnd: raw.planned_end as string | null | undefined,
    confirmedStart: raw.confirmed_start as string | null | undefined,
    confirmedEnd: raw.confirmed_end as string | null | undefined,
    selectedAt: raw.selected_at as string | null | undefined,
    failureReason: raw.failure_reason as string | null | undefined,
    retryCount: Number(raw.retry_count)
  };
}

export async function giveawayPrizes(userId: string): Promise<PrizeAward[]> {
  const reply = await rpc<{ awards?: Record<string, unknown>[] }>(SUBJECT, { user_id: userId }, 3000);
  return (reply.awards ?? []).map(mapAward);
}
