// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { clampInt, REWARD_ON_REDEEM, type RewardOnRedeem } from '@bagel/kit';
import {
  REWARD_COOLDOWN_MAX,
  REWARD_REPLY_MAX,
  isHexColor,
  isValidRewardCost,
  isValidRewardTitle
} from '$lib/reward-fields';

export interface RewardCore {
  title: string;
  cost: number;
  onRedeem: RewardOnRedeem;
  color: string;
  cooldown: number;
  replyMessage: string;
}

export const oneOf =
  <T>(allowed: readonly T[], fallback: T) =>
  (v: unknown): T =>
    allowed.includes(v as T) ? (v as T) : fallback;

export const asOnRedeem = oneOf<RewardOnRedeem>(REWARD_ON_REDEEM, 'fulfill');

export const text =
  (max = Infinity, trim = false) =>
  (v: unknown): string => {
    const s = String(v ?? '');
    return (trim ? s.trim() : s).slice(0, max);
  };

export const flag =
  (dflt: boolean) =>
  (v: unknown): boolean =>
    dflt ? v !== false : v === true;

export const int =
  (min: number, max: number, dflt: number) =>
  (v: unknown): number =>
    clampInt(v, min, max, dflt);

export const hexOrEmpty = (v: unknown): string => {
  const s = text()(v);
  return isHexColor(s) ? s : '';
};

type CoreTextKey = 'title' | 'cost' | 'color' | 'replyMessage';
type CoreRule = (f: FormData, out: Partial<RewardCore>) => string | null;

function rule<K extends CoreTextKey>(
  key: K,
  read: (f: FormData) => RewardCore[K],
  valid: (v: RewardCore[K]) => boolean,
  error: string
): CoreRule {
  return (f, out) => {
    const v = read(f);
    if (!valid(v)) return error;
    out[key] = v;
    return null;
  };
}

const formText = (name: string) => (f: FormData) => String(f.get(name) ?? '').trim();

const CORE_RULES: readonly CoreRule[] = [
  rule('title', formText('title'), isValidRewardTitle, 'Title is required (max 45 characters).'),
  rule('cost', (f) => Math.trunc(Number(f.get('cost'))), isValidRewardCost, 'Enter a valid point cost.'),
  rule('color', formText('color'), (v) => !v || isHexColor(v), 'Pick a valid colour.'),
  rule(
    'replyMessage',
    formText('replyMessage'),
    (v) => v.length <= REWARD_REPLY_MAX,
    'Reply is too long (max 200 characters).'
  )
];

export function parseRewardCore(f: FormData): { draft: RewardCore } | { error: string } {
  const out: Partial<RewardCore> = {};
  for (const run of CORE_RULES) {
    const error = run(f, out);
    if (error) return { error };
  }
  return {
    draft: {
      ...out,
      onRedeem: asOnRedeem(f.get('onRedeem')),
      cooldown: clampInt(f.get('cooldown'), 0, REWARD_COOLDOWN_MAX, 0)
    } as RewardCore
  };
}
