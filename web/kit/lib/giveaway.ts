// Pure rules shared by the giveaway form and its server action.
// Keep these checks independent of Svelte so the no-live-mutation preview can
// be tested without a browser or a Transactions responder.

export type GiveawayEligibility = {
  eligible: number;
  free: number;
  premium: number;
  subscribers: number;
  excluded: number;
};

export type GiveawaySummary = {
  winners: number;
  months: number;
  totalMonths: number | string | null;
};

export type GiveawayCapabilityFlags = {
  newAwardsEnabled: boolean;
  schedulingEnabled: boolean;
  providerMutationsEnabled: boolean;
  intervalRuleVerified: boolean;
};

export const MAX_PRIZE_MONTHS = 12;

export function giveawayDrawState(capabilities?: GiveawayCapabilityFlags): { blocked: boolean; pending: boolean } {
  if (!capabilities) return { blocked: false, pending: false };
  return {
    blocked: !capabilities.newAwardsEnabled,
    pending: capabilities.newAwardsEnabled && (!capabilities.schedulingEnabled || !capabilities.providerMutationsEnabled || !capabilities.intervalRuleVerified)
  };
}

/** Positive whole months within the supported prize duration. */
export function parsePrizeMonths(raw: unknown): number | null {
  const value = typeof raw === 'number' ? raw : Number(String(raw ?? '').trim());
  return Number.isSafeInteger(value) && value > 0 && value <= MAX_PRIZE_MONTHS ? value : null;
}

/** A draw cannot have zero winners and must use a safe integer count. */
export function parseWinnerCount(raw: unknown): number | null {
  const value = typeof raw === 'number' ? raw : Number(String(raw ?? '').trim());
  return Number.isSafeInteger(value) && value > 0 ? value : null;
}

/** Overflow-safe display arithmetic. A null total means the input is too large. */
export function giveawaySummary(winners: unknown, months: unknown): GiveawaySummary {
  const winnerCount = parseWinnerCount(winners) ?? 0;
  const monthCount = parsePrizeMonths(months) ?? 0;
  const total = winnerCount && monthCount ? winnerCount * monthCount : 0;
  return {
    winners: winnerCount,
    months: monthCount,
    totalMonths: Number.isSafeInteger(total) ? total : null
  };
}

export function eligibilityTotal(parts: GiveawayEligibility): number {
  return parts.free + parts.premium + parts.subscribers;
}

export function eligibilityExclusionTotal(parts: GiveawayEligibility): number {
  return Math.max(0, parts.excluded);
}
