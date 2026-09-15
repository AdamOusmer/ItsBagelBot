import { describe, expect, it } from 'bun:test';
import {
  eligibilityExclusionTotal,
  eligibilityTotal,
  giveawaySummary,
  giveawayDrawState,
  parsePrizeMonths,
  parseWinnerCount
} from './giveaway';

describe('giveaway rules', () => {
  it('accepts supported positive whole months and rejects longer prizes', () => {
    expect(parsePrizeMonths('3')).toBe(3);
    expect(parsePrizeMonths(12)).toBe(12);
    expect(parsePrizeMonths(13)).toBeNull();
    expect(parsePrizeMonths('0')).toBeNull();
    expect(parsePrizeMonths('1.5')).toBeNull();
  });

  it('rejects invalid winner counts', () => {
    expect(parseWinnerCount('5')).toBe(5);
    expect(parseWinnerCount('')).toBeNull();
    expect(parseWinnerCount(-1)).toBeNull();
  });

  it('keeps the summary honest when multiplication overflows', () => {
    expect(giveawaySummary(5, 3).totalMonths).toBe(15);
    expect(giveawaySummary(Number.MAX_SAFE_INTEGER, 2).totalMonths).toBeNull();
  });

  it('derives the visible population from account categories', () => {
    const parts = { eligible: 13, free: 7, premium: 4, subscribers: 2, excluded: 9 };
    expect(eligibilityTotal(parts)).toBe(13);
    expect(eligibilityExclusionTotal(parts)).toBe(9);
  });

  it('allows selection while fulfillment gates are pending', () => {
    expect(giveawayDrawState({ newAwardsEnabled: true, schedulingEnabled: false, providerMutationsEnabled: false, intervalRuleVerified: false })).toEqual({ blocked: false, pending: true });
    expect(giveawayDrawState({ newAwardsEnabled: false, schedulingEnabled: true, providerMutationsEnabled: true, intervalRuleVerified: true })).toEqual({ blocked: true, pending: false });
  });
});
