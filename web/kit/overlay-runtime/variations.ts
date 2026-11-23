// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { eventAmount } from './events';
import type { OverlayEvent } from './feed';
import type { AlertConfig, AlertLook, AlertVariation, Condition } from './widget-types';

type Matcher = (event: OverlayEvent, value: AlertVariation['value']) => boolean;
const gifted = new Set<OverlayEvent['kind']>(['gift_sub', 'community_gift']);
const matches: Record<Exclude<Condition, 'random'>, Matcher> = {
  anonymous: (e) => e.anonymous === true,
  is_gift: (e) => gifted.has(e.kind),
  prime: (e) => e.tier === 'prime',
  upgrade: (e) => e.upgrade === true,
  months_exact: (e, v) => v.trim() !== '' && (e.months ?? 0) === Number(v),
  streak_min: (e, v) => v.trim() !== '' && (e.streak ?? 0) >= Number(v),
  amount_min: (e, v) => v.trim() !== '' && eventAmount(e) >= Number(v),
  amount_exact: (e, v) => v.trim() !== '' && eventAmount(e) === Number(v),
  tier: (e, v) => e.tier === v,
  months_min: (e, v) => v.trim() !== '' && (e.months ?? 0) >= Number(v),
  count_min: (e, v) => v.trim() !== '' && eventAmount(e) >= Number(v),
  message_contains: (e, v) => Boolean(v) && (e.message?.text ?? '').toLowerCase().includes(v.toLowerCase())
};

export function selectVariation(config: AlertConfig, event: OverlayEvent, random = Math.random): AlertLook {
  const ordered = config.variations.find((item) => item.condition !== 'random' && matches[item.condition](event, item.value));
  const pool = config.variations.filter((item) => item.condition === 'random');
  const selected = ordered ?? pool[Math.floor(random() * pool.length)];
  const override = selected?.overrides;
  return { ...config, ...override, style: { ...config.style, ...override?.style } };
}
