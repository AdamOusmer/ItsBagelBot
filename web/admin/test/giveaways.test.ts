// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, it, mock } from 'bun:test';
import { actionPayload } from '../../kit/lib/action-result';
import { freezePoolDigest } from '../src/lib/giveaway-workflow';

process.env.NEW_RELIC_ENABLED = 'false';

const requests: { subject: string; body: unknown }[] = [];
let reply: unknown = {};
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, body: unknown) => {
    requests.push({ subject, body });
    return reply;
  }
}));

const { giveawayFreeze, giveawayGet, giveawayPreview, mapDrawMetadata, persistedEligibleCount, selectionMethodForAlgorithm } = await import('../src/lib/server/giveaways');

const campaign = {
  id: 'c-1', title: 'Launch', reason: 'Internal', rules_version: 'v1', winner_count: 2, prize_months: 3,
  status: 'frozen', created_by: 7, version: 4, created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-02T00:00:00Z'
};
const award = {
  id: 'a-1', campaign_id: 'c-1', draw_id: 'd-1', user_id: 42, ordinal: 1, prize_months: 3,
  interval_rule: 'after_current_term', state: 'needs_review', billing_state: 'uncertain', email_state: 'missing_contact',
  retry_count: 1, version: 2, selected_at: '2026-09-02T00:00:00Z', updated_at: '2026-09-02T00:00:00Z'
};

describe('giveaway RPC wire mapping', () => {
  it.each([
    { name: 'a candidate without a username falls back to the user id', candidates: [], login: '42' },
    { name: 'a candidate username becomes the winner login', candidates: [{ user_id: 42, username: 'live_winner', eligible: true }], login: 'live_winner' }
  ])('keeps the exact campaign and award fields without inventing dates: $name', async ({ candidates, login }) => {
    reply = { campaign, awards: [award], candidates };
    const detail = await giveawayGet({ actorId: 'admin-1', campaignId: 'c-1' });
    expect(detail).toMatchObject({ id: 'c-1', status: 'frozen', version: 4 });
    expect(detail.winners[0]).toMatchObject({ userId: '42', login, emailState: 'missing_contact', startAt: undefined, endAt: undefined });
  });

  it('preserves an explicit operation key and expected version for retries', async () => {
    await giveawayFreeze({ actorId: 'admin-1', campaignId: 'c-1', poolDigest: 'sha256:preview', idempotencyKey: 'c-1:freeze:4', expectedVersion: 4 });
    expect(requests.at(-1)).toEqual({
      subject: 'bagel.rpc.admin.giveaways.freeze',
      body: { actor_id: 'admin-1', idempotency_key: 'c-1:freeze:4', expected_version: 4, campaign_id: 'c-1', pool_digest: 'sha256:preview' }
    });
  });

  it('carries the authoritative preview digest into freeze and rejects a missing snapshot', () => {
    expect(freezePoolDigest({ poolDigest: 'sha256:preview' })).toBe('sha256:preview');
    expect(freezePoolDigest({ poolDigest: '  ' })).toBeNull();
  });

  it('maps the authoritative exclusion breakdown instead of counting eligible candidates', async () => {
    reply = {
      summary: {
        total: 10, eligible: 4, excluded: 6, free: 2, premium: 1, subscribers: 1,
        requested_winners: 1, prize_months: 3, total_prize_months: '3',
        exclusions: { banned: 1, inactive: 1, not_onboarded: 1, test_account: 1, vip: 1, current_staff: 1 }
      }, pool_digest: 'sha256:preview', capabilities: {
        new_awards_enabled: true, scheduling_enabled: false, provider_mutations_enabled: false, interval_rule_verified: false
      }
    };
    const preview = await giveawayPreview({ actorId: 'admin-1', winnerCount: 1, prizeMonths: 3 });
    expect(preview.exclusions).toEqual({ banned: 1, inactive: 1, not_onboarded: 1, test_account: 1, vip: 1, current_staff: 1 });
    expect(preview.poolDigest).toBe('sha256:preview');
  });

  it('keeps draw algorithm metadata separate from the selection method', () => {
    expect(mapDrawMetadata({ pool_digest: 'sha256:draw', algorithm_version: 'fisher-yates-v1' })).toEqual({
      poolDigest: 'sha256:draw', algorithmVersion: 'fisher-yates-v1', selectionMethod: undefined
    });
    expect(selectionMethodForAlgorithm('crypto-rand-partial-fisher-yates-v1')).toBe('random_draw');
    expect(selectionMethodForAlgorithm('unknown')).toBeUndefined();
  });

  it('projects the persisted eligible count only for a saved pool', () => {
    expect(persistedEligibleCount('frozen', [{ eligible: true }, { eligible: false }, { eligible: true }])).toBe(2);
    expect(persistedEligibleCount('draft', [{ eligible: true }])).toBeUndefined();
  });

  it('consumes the SvelteKit enhanced form result envelope', () => {
    const result = { type: 'success', data: { preview: { poolDigest: 'sha256:preview' } } };
    expect(actionPayload<typeof result.data>(result)?.preview.poolDigest).toBe('sha256:preview');
  });
});
