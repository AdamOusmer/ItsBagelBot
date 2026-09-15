import { rpc } from '@bagel/kit/server/nats';
import type { GiveawayEligibility, GiveawaySummary } from '@bagel/kit/giveaway';
import { randomUUID } from 'node:crypto';

const READ_TIMEOUT = 5000;

export type GiveawayStatus = 'draft' | 'frozen' | 'drawn' | 'cancelled' | 'complete';
export type AwardState = 'selected' | 'preparing' | 'needs_review' | 'scheduled' | 'active' | 'completed' | 'voided';
export type BillingState = 'not_required' | 'pending' | 'protected' | 'uncertain' | 'incident' | 'reconciled';
export type EmailState = 'queued' | 'accepted' | 'missing_contact' | 'failed' | 'uncertain';

export type GiveawayCapabilities = {
  newAwardsEnabled: boolean;
  schedulingEnabled: boolean;
  providerMutationsEnabled: boolean;
  intervalRuleVerified: boolean;
  reason?: string;
};

export type GiveawayWire = {
  id: string; title: string; reason: string; status: GiveawayStatus; winnerCount: number; prizeMonths: number;
  summary?: GiveawaySummary; eligible?: GiveawayEligibility; createdAt: string; drawnAt?: string | null;
  pendingAwards?: number; version: number; capabilities?: GiveawayCapabilities;
};

export type GiveawayWinnerWire = {
  id: string; userId: string; login: string; displayName?: string; selectedAt: string; prizeMonths: number;
  awardState: AwardState; billingState: BillingState; startAt?: string | null; endAt?: string | null;
  paidThroughAt?: string | null; nextChargeAt?: string | null; providerVerifiedAt?: string | null;
  emailState: EmailState; emailWarning?: string | null; failureReason?: string | null; retryCount: number;
};

export type GiveawayAlertWire = {
  id: string; awardId: string; reason: string; pendingSince: string; upcomingChargeAt?: string | null; urgent?: boolean;
};

export type GiveawayDetailWire = GiveawayWire & {
  winners: GiveawayWinnerWire[]; eligible?: GiveawayEligibility; poolDigest?: string | null;
  eligibleCount?: number; selectionMethod?: 'random_draw'; algorithmVersion?: string | null; alerts?: GiveawayAlertWire[];
};

export type GiveawayPreviewWire = {
  eligible: GiveawayEligibility; exclusions: Record<string, number>; summary: GiveawaySummary;
  durationProtectionWarnings?: string[]; frozen?: boolean; poolDigest?: string; capabilities?: GiveawayCapabilities;
};

export type FreezeOperation = { actorId: string; campaignId: string; poolDigest: string; expectedVersion: number; idempotencyKey: string };
export type DrawOperation = { actorId: string; campaignId: string; expectedVersion: number; idempotencyKey: string };

type CampaignRaw = {
  id: string; title: string; reason?: string; rules_version: string; winner_count: number; prize_months: number;
  status: GiveawayStatus; created_by: number; version: number; created_at: string; updated_at: string;
  frozen_at?: string; drawn_at?: string;
};
type AwardRaw = {
  id: string; campaign_id: string; draw_id: string; user_id: number; ordinal: number; prize_months: number;
  interval_rule: string; state: AwardState; billing_state: BillingState; email_state: EmailState;
  planned_start?: string; planned_end?: string; confirmed_start?: string; confirmed_end?: string;
  grant_id?: string; billing_operation_id?: string; failure_reason?: string; retry_count: number;
  version: number; selected_at: string; updated_at: string;
};
type CandidateRaw = { id?: string; user_id: number; username?: string; eligible: boolean; exclusion_reason?: string; eligibility?: unknown };
type PoolExclusionCountsRaw = {
  banned: number; inactive: number; not_onboarded: number; test_account: number; vip: number; current_staff: number;
};
type PoolSummaryRaw = {
  total: number; eligible: number; excluded: number; free: number; premium: number; subscribers: number;
  requested_winners: number; prize_months: number; total_prize_months: string; exclusions: PoolExclusionCountsRaw;
};
type CapabilitiesRaw = {
  new_awards_enabled: boolean; scheduling_enabled: boolean; provider_mutations_enabled: boolean;
  interval_rule_verified: boolean; reason?: string;
};
type AlertRaw = {
  id: string; award_id: string; operation_id?: string; category: string; state: string; message: string;
  affected_boundary?: string; first_seen_at: string; last_seen_at: string; acknowledged_by?: number;
  acknowledged_at?: string; resolved_at?: string;
};
type CampaignReply = { campaign?: CampaignRaw; capabilities?: CapabilitiesRaw };
type ListReply = { campaigns?: CampaignRaw[]; capabilities?: CapabilitiesRaw };
type PreviewReply = { campaign?: CampaignRaw; candidates?: CandidateRaw[]; summary: PoolSummaryRaw; pool_digest?: string; capabilities: CapabilitiesRaw };
type DrawRaw = { pool_digest: string; algorithm_version: string };
type GetReply = { campaign?: CampaignRaw; awards?: AwardRaw[]; candidates?: CandidateRaw[]; draw?: DrawRaw; capabilities?: CapabilitiesRaw };
type DrawReply = { awards?: AwardRaw[]; draw?: DrawRaw; capabilities?: CapabilitiesRaw };
type MutationInput = { actorId: string; idempotencyKey?: string; expectedVersion?: number; reason?: string };

export function buildMutation(input: MutationInput): Record<string, string | number> {
  return {
    actor_id: input.actorId, idempotency_key: input.idempotencyKey ?? randomUUID(),
    ...(input.expectedVersion === undefined ? {} : { expected_version: input.expectedVersion }),
    ...(input.reason ? { reason: input.reason } : {})
  };
}

function capabilities(raw?: CapabilitiesRaw): GiveawayCapabilities | undefined {
  if (!raw) return undefined;
  return {
    newAwardsEnabled: raw.new_awards_enabled, schedulingEnabled: raw.scheduling_enabled,
    providerMutationsEnabled: raw.provider_mutations_enabled, intervalRuleVerified: raw.interval_rule_verified, reason: raw.reason
  };
}

export function mapCampaign(raw: CampaignRaw): GiveawayWire {
  return {
    id: raw.id, title: raw.title, reason: raw.reason ?? '', status: raw.status, winnerCount: raw.winner_count,
    prizeMonths: raw.prize_months, createdAt: raw.created_at, drawnAt: raw.drawn_at, version: raw.version
  };
}

function eligibility(summary: PoolSummaryRaw): GiveawayEligibility {
  return { eligible: summary.eligible, free: summary.free, premium: summary.premium, subscribers: summary.subscribers, excluded: summary.excluded };
}

function summary(raw: PoolSummaryRaw): GiveawaySummary {
  const numericTotal = Number(raw.total_prize_months);
  return { winners: raw.requested_winners, months: raw.prize_months, totalMonths: Number.isSafeInteger(numericTotal) ? numericTotal : raw.total_prize_months };
}

export function mapAward(raw: AwardRaw, login = String(raw.user_id)): GiveawayWinnerWire {
  return {
    id: raw.id, userId: String(raw.user_id), login, selectedAt: raw.selected_at,
    prizeMonths: raw.prize_months, awardState: raw.state, billingState: raw.billing_state,
    startAt: raw.confirmed_start ?? raw.planned_start, endAt: raw.confirmed_end ?? raw.planned_end,
    emailState: raw.email_state, failureReason: raw.failure_reason, retryCount: raw.retry_count
  };
}

const DRAW_ALGORITHM = 'crypto-rand-partial-fisher-yates-v1';

export function persistedEligibleCount(status: GiveawayStatus, candidates: ReadonlyArray<{ eligible: boolean }>): number | undefined {
  if (!['frozen', 'drawn', 'cancelled', 'complete'].includes(status) || candidates.length === 0) return undefined;
  return candidates.filter((candidate) => candidate.eligible).length;
}

export function selectionMethodForAlgorithm(algorithmVersion?: string): 'random_draw' | undefined {
  return algorithmVersion === DRAW_ALGORITHM ? 'random_draw' : undefined;
}

export function mapDrawMetadata(draw?: DrawRaw): { poolDigest?: string; algorithmVersion?: string; selectionMethod?: 'random_draw' } {
  if (!draw) return {};
  return {
    poolDigest: draw.pool_digest,
    algorithmVersion: draw.algorithm_version,
    selectionMethod: selectionMethodForAlgorithm(draw.algorithm_version)
  };
}

export function mapPreview(reply: PreviewReply): GiveawayPreviewWire {
  return {
    eligible: eligibility(reply.summary), exclusions: { ...reply.summary.exclusions }, summary: summary(reply.summary),
    poolDigest: reply.pool_digest, capabilities: capabilities(reply.capabilities)
  };
}

function mapAlert(raw: AlertRaw): GiveawayAlertWire {
  return { id: raw.id, awardId: raw.award_id, reason: raw.message, pendingSince: raw.first_seen_at, upcomingChargeAt: raw.affected_boundary };
}

function requireCampaign(campaign: CampaignRaw | undefined): CampaignRaw {
  if (!campaign) throw new Error('Giveaway service returned no campaign.');
  return campaign;
}

export async function giveawaysList(input: { actorId: string }): Promise<GiveawayWire[]> {
  const reply = await rpc<ListReply>('bagel.rpc.admin.giveaways.list', { ...buildMutation(input), limit: 100 }, READ_TIMEOUT);
  return (reply.campaigns ?? []).map(mapCampaign);
}

export async function giveawayGet(input: { actorId: string; campaignId: string }): Promise<GiveawayDetailWire> {
  const reply = await rpc<GetReply>('bagel.rpc.admin.giveaways.get', { ...buildMutation(input), campaign_id: input.campaignId }, READ_TIMEOUT);
  const campaign = mapCampaign(requireCampaign(reply.campaign));
  const logins = new Map((reply.candidates ?? []).map((candidate) => [String(candidate.user_id), candidate.username ?? String(candidate.user_id)]));
  const draw = mapDrawMetadata(reply.draw);
  return {
    ...campaign, winners: (reply.awards ?? []).map((award) => mapAward(award, logins.get(String(award.user_id)))),
    ...draw, eligibleCount: persistedEligibleCount(campaign.status, reply.candidates ?? []),
    capabilities: capabilities(reply.capabilities)
  };
}

export type GiveawayPreviewInput =
  | { actorId: string; campaignId: string }
  | { actorId: string; winnerCount: number; prizeMonths: number };

export async function giveawayPreview(input: GiveawayPreviewInput): Promise<GiveawayPreviewWire> {
  const request = {
    ...buildMutation(input),
    ...('campaignId' in input ? { campaign_id: input.campaignId } : { winner_count: input.winnerCount, prize_months: input.prizeMonths })
  };
  const reply = await rpc<PreviewReply>('bagel.rpc.admin.giveaways.preview', request, READ_TIMEOUT);
  return mapPreview(reply);
}

export async function giveawayFreeze(operation: FreezeOperation): Promise<void> {
  await rpc('bagel.rpc.admin.giveaways.freeze', { ...buildMutation(operation), campaign_id: operation.campaignId, pool_digest: operation.poolDigest }, READ_TIMEOUT);
}

export async function giveawayCreate(input: { actorId: string; title: string; reason: string; winnerCount: number; prizeMonths: number }): Promise<GiveawayWire> {
  const reply = await rpc<CampaignReply>('bagel.rpc.admin.giveaways.create', {
    ...buildMutation(input), title: input.title, reason: input.reason, winner_count: input.winnerCount, prize_months: input.prizeMonths,
  }, READ_TIMEOUT);
  return mapCampaign(requireCampaign(reply.campaign));
}

export async function giveawayDraw(operation: DrawOperation): Promise<{ awards: GiveawayWinnerWire[] }> {
  const reply = await rpc<DrawReply>('bagel.rpc.admin.giveaways.draw', { ...buildMutation(operation), campaign_id: operation.campaignId }, READ_TIMEOUT);
  return { awards: (reply.awards ?? []).map((award) => mapAward(award)) };
}

export async function giveawayRetry(input: { actorId: string; awardId: string }): Promise<void> {
  await rpc('bagel.rpc.admin.giveaways.retry', { ...buildMutation(input), award_id: input.awardId }, READ_TIMEOUT);
}

export async function giveawayAlerts(input: { actorId: string }): Promise<GiveawayAlertWire[]> {
  const reply = await rpc<{ alerts?: AlertRaw[] }>('bagel.rpc.admin.giveaways.alerts', { ...buildMutation(input), unresolved_only: true, limit: 100 }, READ_TIMEOUT);
  return (reply.alerts ?? []).map(mapAlert);
}

export async function giveawayHistory(input: { actorId: string; userId: string }): Promise<GiveawayWinnerWire[]> {
  const reply = await rpc<{ awards?: AwardRaw[] }>('bagel.rpc.admin.giveaways.history', { ...buildMutation(input), user_id: input.userId }, READ_TIMEOUT);
  return (reply.awards ?? []).map((award) => mapAward(award));
}
